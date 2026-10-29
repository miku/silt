package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests run the test binary itself as the browser: with SILT_FAKE_BROWSER
// set, TestMain acts as one (fakeBrowser) instead of running the tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SILT_FAKE_BROWSER"); mode != "" {
		os.Exit(fakeBrowser(mode, os.Getenv("SILT_FAKE_BROWSER_DIR"), os.Args[1:]))
	}
	os.Exit(m.Run())
}

const fakePage = "<html><head><title>Search results</title></head><body>rendered</body></html>"

// fakeBrowser is the fake browser. In dir it writes the flags it was given
// to "args", and holds "running" while it runs, failing if another fake
// browser already holds it. mode is one of:
//
//	page    print fakePage after 50 ms
//	fail    log noise and a last line to stderr, and exit 3
//	hang    sleep for a minute
//	in-use  exit with profileInUse twice, then act as page
func fakeBrowser(mode, dir string, args []string) int {
	if mode == "in-use" {
		count := filepath.Join(dir, "in-use-count")
		b, _ := os.ReadFile(count)
		if len(b) < 2 {
			_ = os.WriteFile(count, append(b, 'x'), 0o600)
			return profileInUse
		}
	}
	running := filepath.Join(dir, "running")
	f, err := os.OpenFile(running, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "two browsers at once")
		return 99
	}
	f.Close()
	defer os.Remove(running)
	_ = os.WriteFile(filepath.Join(dir, "args"), []byte(strings.Join(args, "\n")), 0o600)
	switch mode {
	case "fail":
		fmt.Fprint(os.Stderr, "[1:1:ERROR:gpu_init.cc] noise\nMissing X server or $DISPLAY\n\n")
		return 3
	case "hang":
		time.Sleep(time.Minute)
		return 0
	}
	time.Sleep(50 * time.Millisecond)
	fmt.Print(fakePage)
	return 0
}

// fake returns a Browser that runs fakeBrowser in mode, with a fresh profile,
// and the directory the fake browser writes to.
func fake(t *testing.T, mode string) (*Browser, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("SILT_FAKE_BROWSER", mode)
	t.Setenv("SILT_FAKE_BROWSER_DIR", dir)
	return &Browser{Path: exe, ProfileDir: filepath.Join(t.TempDir(), "profile")}, dir
}

func fakeArgs(t *testing.T, dir string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

const pageURL = "https://pypi.org/search/?q=kill+port"

func TestDumpDOMRunsTheBrowserWithCloakBrowsersFlags(t *testing.T) {
	b, dir := fake(t, "page")
	dom, err := b.DumpDOM(t.Context(), pageURL)
	if err != nil || dom != fakePage {
		t.Fatalf("DumpDOM = %q, %v", dom, err)
	}
	args := fakeArgs(t, dir)
	seed, err := b.seed()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--headless=new",
		"--no-sandbox",
		"--fingerprint=" + strconv.Itoa(seed),
		"--user-data-dir=" + b.ProfileDir,
		"--virtual-time-budget=15000",
		"--dump-dom",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("flag %s missing from %q", want, args)
		}
	}
	if !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--fingerprint-platform=") }) {
		t.Errorf("no --fingerprint-platform in %q", args)
	}
	if args[len(args)-1] != pageURL {
		t.Errorf("the URL should come last: %q", args)
	}
}

func TestTheSeedIsKeptInTheProfile(t *testing.T) {
	b, dir := fake(t, "page")
	path := filepath.Join(b.ProfileDir, seedFile)
	fingerprint := func() string {
		t.Helper()
		if _, err := b.DumpDOM(t.Context(), pageURL); err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(fakeArgs(t, dir), func(a string) bool { return strings.HasPrefix(a, "--fingerprint=") })
		return fakeArgs(t, dir)[i]
	}
	first := fingerprint()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The wrappers' format: digits and a newline.
	if s := string(data); "--fingerprint="+strings.TrimSuffix(s, "\n") != first || !strings.HasSuffix(s, "\n") {
		t.Errorf("seed file %q, flag %s", s, first)
	}
	if fi, err := os.Stat(b.ProfileDir); err != nil {
		t.Error(err)
	} else if fi.Mode().Perm() != 0o700 && runtime.GOOS != "windows" {
		t.Errorf("profile mode %v; want a directory only its owner can read", fi.Mode())
	}
	if again := fingerprint(); again != first {
		t.Errorf("the second run used %s, the first %s", again, first)
	}

	// A seed the wrappers wrote is used as it is; a corrupt one is replaced.
	if err := os.WriteFile(path, []byte("54321\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fingerprint(); got != "--fingerprint=54321" {
		t.Errorf("got %s, want the stored seed", got)
	}
	for _, bad := range []string{"", "seed\n", "9999\n", "100000\n", "-5\n"} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		n, err := strconv.Atoi(strings.TrimPrefix(fingerprint(), "--fingerprint="))
		if err != nil || n < seedMin || n > seedMax {
			t.Errorf("after a seed file %q: seed %d, %v", bad, n, err)
		}
	}
}

func TestOneBrowserRunsAtATime(t *testing.T) {
	b, _ := fake(t, "page")
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { _, errs[i] = b.DumpDOM(t.Context(), pageURL) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Errorf("concurrent calls: %v", err)
	}
}

func TestWaitingForTheBrowserCountsAgainstTheContext(t *testing.T) {
	b, _ := fake(t, "page")
	sem <- struct{}{} // another search holds the browser
	defer func() { <-sem }()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := b.DumpDOM(ctx, pageURL); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "waiting for the browser") {
		t.Errorf("err = %v", err)
	}
}

func TestAHangingBrowserIsKilledAtTheDeadline(t *testing.T) {
	b, _ := fake(t, "hang")
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := b.DumpDOM(ctx, pageURL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("DumpDOM returned after %v", d)
	}
}

func TestAFailingBrowserSaysWhy(t *testing.T) {
	b, _ := fake(t, "fail")
	_, err := b.DumpDOM(t.Context(), pageURL)
	if err == nil || !strings.Contains(err.Error(), "exit status 3: Missing X server or $DISPLAY") {
		t.Errorf("err = %v; want the exit status and the browser's last line", err)
	}
}

func TestAProfileInUseIsWaitedFor(t *testing.T) {
	b, _ := fake(t, "in-use")
	defer func(d time.Duration) { profileRetryDelay = d }(profileRetryDelay)
	profileRetryDelay = time.Millisecond
	if dom, err := b.DumpDOM(t.Context(), pageURL); err != nil || dom != fakePage {
		t.Errorf("DumpDOM = %q, %v; want the page once the profile is free", dom, err)
	}
}

func TestAProfileThatStaysInUseIsAnError(t *testing.T) {
	b, dir := fake(t, "in-use")
	// Never free: the fake browser exits with profileInUse while it has
	// fewer than two marks.
	if err := os.WriteFile(filepath.Join(dir, "in-use-count"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	defer func(d time.Duration) { profileRetryDelay = d }(profileRetryDelay)
	profileRetryDelay = time.Hour
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if _, err := b.DumpDOM(ctx, pageURL); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "another process is using") {
		t.Errorf("err = %v", err)
	}
}

// touch creates path, and its directory, as an empty file.
func touch(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFind(t *testing.T) {
	exe := touch(t, filepath.Join(t.TempDir(), "chrome"))
	other := touch(t, filepath.Join(t.TempDir(), "chrome"))
	missing := filepath.Join(t.TempDir(), "no-such-chrome")

	cache := t.TempDir()
	installed := func(name string) string { return filepath.Join(cache, name, executable()) }
	touch(t, installed("chromium-145.0.7632.1"))
	touch(t, installed("chromium-146.0.7680.99.5"))
	newest := touch(t, installed("chromium-146.0.7680.177.5"))
	touch(t, filepath.Join(cache, "chromium-147.0.1.1", "README")) // no executable
	touch(t, filepath.Join(cache, "chromium-999.0"))               // a file, not a directory
	touch(t, installed("chromium-latest"))
	touch(t, installed("chrome-200.0"))
	pro := t.TempDir()
	touch(t, filepath.Join(pro, "chromium-146.0.7680.177.5", executable()))
	newestPro := touch(t, filepath.Join(pro, "chromium-146.0.7680.177.6-pro", executable()))

	for _, tt := range []struct {
		name    string
		env     map[string]string
		want    string
		wantErr string
	}{
		{name: "SILT_BROWSER", env: map[string]string{"SILT_BROWSER": exe, "CLOAKBROWSER_BINARY_PATH": other, "CLOAKBROWSER_CACHE_DIR": cache}, want: exe},
		{name: "SILT_BROWSER=off", env: map[string]string{"SILT_BROWSER": "off", "CLOAKBROWSER_BINARY_PATH": other, "CLOAKBROWSER_CACHE_DIR": cache}},
		{name: "SILT_BROWSER missing", env: map[string]string{"SILT_BROWSER": missing, "CLOAKBROWSER_CACHE_DIR": cache}, wantErr: "SILT_BROWSER: "},
		{name: "SILT_BROWSER a directory", env: map[string]string{"SILT_BROWSER": cache}, wantErr: "SILT_BROWSER: " + cache + " is a directory"},
		{name: "empty SILT_BROWSER is unset", env: map[string]string{"SILT_BROWSER": "", "CLOAKBROWSER_BINARY_PATH": other}, want: other},
		{name: "CLOAKBROWSER_BINARY_PATH", env: map[string]string{"CLOAKBROWSER_BINARY_PATH": other, "CLOAKBROWSER_CACHE_DIR": cache}, want: other},
		{name: "CLOAKBROWSER_BINARY_PATH missing", env: map[string]string{"CLOAKBROWSER_BINARY_PATH": missing}, wantErr: "CLOAKBROWSER_BINARY_PATH: "},
		{name: "newest installed", env: map[string]string{"CLOAKBROWSER_CACHE_DIR": cache}, want: newest},
		{name: "a pro build", env: map[string]string{"CLOAKBROWSER_CACHE_DIR": pro}, want: newestPro},
		{name: "nothing installed", env: map[string]string{"CLOAKBROWSER_CACHE_DIR": t.TempDir()}},
		{name: "no cache directory", env: map[string]string{"CLOAKBROWSER_CACHE_DIR": missing}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Find(func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok })
			if tt.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
					t.Errorf("Find = %q, %v; want an error starting %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Find = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
