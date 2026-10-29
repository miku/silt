// Package browser renders pages in CloakBrowser, a patched Chromium, for the
// sources whose search page walls off clients that aren't browsers. PyPI is
// one: it answers silt's plain request with a JavaScript challenge.
//
// silt neither bundles nor installs the browser; its license forbids
// redistribution. [Find] looks for one the user installed
// (`uv tool install cloakbrowser`, `cloakbrowser install`), and without one
// nothing changes.
//
// The browser is run once per page with --dump-dom: it loads the page, lets
// its scripts run, prints the rendered DOM and exits. There is no CDP and no
// Playwright, since a search page only has to be loaded.
package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Find returns the browser silt should use, or "" for none. The first of
// these wins:
//
//  1. SILT_BROWSER: the path of the browser's executable, or "off" for none;
//  2. CLOAKBROWSER_BINARY_PATH, which CloakBrowser's wrappers also honour;
//  3. the newest chromium-* directory under CLOAKBROWSER_CACHE_DIR, or else
//     under ~/.cloakbrowser, where `cloakbrowser install` puts it.
//
// A path set in one of the two variables must name a file; otherwise Find
// returns an error. Not finding a browser in the cache directory is no error.
func Find(lookupEnv func(string) (string, bool)) (string, error) {
	for _, name := range []string{"SILT_BROWSER", "CLOAKBROWSER_BINARY_PATH"} {
		v, _ := lookupEnv(name)
		switch {
		case v == "":
			continue
		case name == "SILT_BROWSER" && v == "off":
			return "", nil
		}
		if fi, err := os.Stat(v); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		} else if fi.IsDir() {
			return "", fmt.Errorf("%s: %s is a directory, not the browser's executable", name, v)
		}
		return v, nil
	}
	dir, _ := lookupEnv("CLOAKBROWSER_CACHE_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil
		}
		dir = filepath.Join(home, ".cloakbrowser")
	}
	return newestInstalled(dir), nil
}

// newestInstalled returns the executable of the newest chromium-<version>
// directory under dir, or "" if there is none. A -pro suffix, for the paid
// build, is allowed.
func newestInstalled(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best []int
	var path string
	for _, e := range entries {
		v, ok := strings.CutPrefix(e.Name(), "chromium-")
		if !ok || !e.IsDir() {
			continue
		}
		version, ok := parseVersion(strings.TrimSuffix(v, "-pro"))
		if !ok || slices.Compare(version, best) <= 0 {
			continue
		}
		exe := filepath.Join(dir, e.Name(), executable())
		if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
			continue
		}
		best, path = version, exe
	}
	return path
}

// parseVersion parses a dotted version such as 146.0.7680.177.5.
func parseVersion(s string) ([]int, bool) {
	var v []int
	for part := range strings.SplitSeq(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		v = append(v, n)
	}
	return v, true
}

// executable is the browser's executable inside a chromium-* directory.
func executable() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join("Chromium.app", "Contents", "MacOS", "Chromium")
	case "windows":
		return "chrome.exe"
	}
	return "chrome"
}

// DefaultProfileDir is the browser profile silt keeps:
// <UserCacheDir>/silt/browser, next to the result cache.
func DefaultProfileDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "silt", "browser"), nil
}

// A Browser runs CloakBrowser to render pages.
type Browser struct {
	// Path is the browser's executable.
	Path string
	// ProfileDir is the browser's profile (--user-data-dir), kept between
	// runs so that a site's challenge cookie is too. It also holds the
	// fingerprint seed, so the browser looks the same to a site every time.
	// It is created if it doesn't exist.
	ProfileDir string
}

// sem lets one browser run at a time in this process: the free build of
// CloakBrowser allows one session at a time.
var sem = make(chan struct{}, 1)

// virtualTimeBudget is how long a page may run its scripts before its DOM is
// dumped. It is virtual time, which the browser fast-forwards while the page
// is idle: PyPI's challenge, which sets a cookie and reloads, passes in about
// 3 s of real time. Without a budget the DOM is dumped before the challenge
// has run.
const virtualTimeBudget = 15 * time.Second

// profileInUse is the browser's exit code when another browser holds the
// profile (Chromium's RESULT_CODE_PROFILE_IN_USE). It exits at once and
// leaves the profile as it was.
const profileInUse = 21

// profileRetryDelay is the pause before trying a profile again that another
// process is using.
var profileRetryDelay = 250 * time.Millisecond

// errProfileInUse is returned by run when another browser holds the profile.
var errProfileInUse = errors.New("the browser profile is in use")

// DumpDOM loads url and returns the DOM the page rendered, once its scripts
// have run.
//
// One browser runs at a time in this process; a call waits for the one
// before it. If another process's browser holds the profile, DumpDOM waits
// for it too. Either wait counts against ctx, and so does the page load: when
// ctx is done, the browser is killed.
func (b *Browser) DumpDOM(ctx context.Context, url string) (string, error) {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for the browser: %w", ctx.Err())
	}
	defer func() { <-sem }()

	seed, err := b.seed()
	if err != nil {
		return "", err
	}
	platform := "windows"
	if runtime.GOOS == "darwin" {
		// On macOS CloakBrowser's wrappers present it as the Mac browser it
		// is: a Windows fingerprint on a Mac is detectable.
		platform = "macos"
	}
	// The flags CloakBrowser's wrappers launch it with, plus the dump.
	args := []string{
		"--headless=new",
		"--no-sandbox",
		"--fingerprint=" + strconv.Itoa(seed),
		"--fingerprint-platform=" + platform,
		"--user-data-dir=" + b.ProfileDir,
		"--virtual-time-budget=" + strconv.FormatInt(virtualTimeBudget.Milliseconds(), 10),
		"--dump-dom",
		url,
	}
	for {
		dom, err := b.run(ctx, args)
		if !errors.Is(err, errProfileInUse) {
			return dom, err
		}
		select {
		case <-time.After(profileRetryDelay):
		case <-ctx.Done():
			return "", fmt.Errorf("waiting for the browser profile %s, which another process is using: %w", b.ProfileDir, ctx.Err())
		}
	}
}

// run runs the browser once with args and returns what it printed.
func (b *Browser) run(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, b.Path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// Don't wait long for a child process that kept stdout open after the
	// browser was killed.
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("the browser: %w", ctx.Err())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == profileInUse {
		return "", errProfileInUse
	}
	if err != nil {
		// The browser logs a lot, even when all goes well; the last line is
		// the likeliest to say what went wrong.
		if last := lastLine(stderr.String()); last != "" {
			return "", fmt.Errorf("the browser: %w: %s", err, last)
		}
		return "", fmt.Errorf("the browser: %w", err)
	}
	return stdout.String(), nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(s[strings.LastIndexByte(s, '\n')+1:])
}

// The fingerprint seed is kept in the profile, in the file and format
// CloakBrowser's wrappers use: a decimal number in [seedMin, seedMax] and a
// newline. A profile then keeps its seed whichever of them opens it.
const (
	seedFile         = ".cloakbrowser-seed"
	seedMin, seedMax = 10000, 99999
)

// seed returns the profile's fingerprint seed, creating the profile and the
// seed on first use. A seed file that doesn't hold a valid seed is replaced.
func (b *Browser) seed() (int, error) {
	path := filepath.Join(b.ProfileDir, seedFile)
	if data, err := os.ReadFile(path); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && n >= seedMin && n <= seedMax {
			return n, nil
		}
	}
	// Owner only, like the result cache: the profile holds a site's cookies.
	if err := os.MkdirAll(b.ProfileDir, 0o700); err != nil {
		return 0, fmt.Errorf("the browser profile: %w", err)
	}
	n := seedMin + rand.IntN(seedMax-seedMin+1)
	f, err := os.CreateTemp(b.ProfileDir, seedFile+".*.tmp")
	if err != nil {
		return 0, fmt.Errorf("the browser profile: %w", err)
	}
	_, err = fmt.Fprintf(f, "%d\n", n)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return 0, fmt.Errorf("the browser profile: %w", err)
	}
	return n, nil
}
