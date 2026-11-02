package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// siltBin is the silt binary under test, built once for the package.
var siltBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "silt-cli-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	siltBin = filepath.Join(dir, "silt")
	if runtime.GOOS == "windows" {
		siltBin += ".exe" // exec finds nothing else there
	}
	if out, err := exec.CommandContext(context.Background(), "go", "build", "-o", siltBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building silt: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// runSilt runs the binary with args. Its stdout is a pipe, never a terminal.
func runSilt(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), siltBin, args...)
	// No config file: the user config directory on Linux and on Windows. No
	// result or catalog cache either, so a live test always searches the
	// registries, and no browser or PyPI index, so it searches them the way
	// patent does.
	config := t.TempDir()
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "XDG_CONFIG_HOME="+config, "AppData="+config,
		"SILT_CACHE_TTL=0", "SILT_CATALOG_TTL=0", "SILT_BROWSER=off", "SILT_PYPI_INDEX=off")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		return out.String(), errOut.String(), exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), 0
}

func TestNoArgsPipedStdoutExitsWithError(t *testing.T) {
	_, stderr, code := runSilt(t)
	if code == 0 {
		t.Error("should fail when stdout is piped")
	}
	if !strings.Contains(stderr, "No idea provided") && !strings.Contains(stderr, "Usage") {
		t.Errorf("stderr should mention the missing idea, got: %s", stderr)
	}
}

func TestJSONFlagWithoutIdeaExitsWithError(t *testing.T) {
	_, stderr, code := runSilt(t, "--json")
	if code == 0 || !strings.Contains(stderr, "--json requires an idea") {
		t.Errorf("exit %d, stderr: %s", code, stderr)
	}
}

func TestHelpFlagExitsSuccess(t *testing.T) {
	stdout, _, code := runSilt(t, "--help")
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(stdout, "[IDEA]") {
		t.Error("the idea should be shown as optional")
	}
	if !strings.Contains(stdout, "interactive") {
		t.Error("help should mention interactive search")
	}
}

func TestVersionFlagExitsSuccess(t *testing.T) {
	for _, flag := range []string{"--version", "-V"} {
		stdout, _, code := runSilt(t, flag)
		if code != 0 || !strings.HasPrefix(stdout, "silt ") {
			t.Errorf("%s: exit %d, stdout %q; the version output should name the binary", flag, code, stdout)
		}
	}
}

func TestTooVagueIdeaExitsWithError(t *testing.T) {
	_, stderr, code := runSilt(t, "hi")
	if code == 0 || (!strings.Contains(stderr, "Too vague") && !strings.Contains(stderr, "describe")) {
		t.Errorf("should reject a one-word idea: exit %d, stderr: %s", code, stderr)
	}
}

func TestEmptyIdeaExitsWithError(t *testing.T) {
	if _, _, code := runSilt(t, ""); code == 0 {
		t.Error("an empty idea must fail")
	}
}

func TestListSourcesPrintsEveryKebabAndDisplayName(t *testing.T) {
	stdout, _, code := runSilt(t, "--list-sources")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 21 {
		t.Fatalf("got %d lines, want 21", len(lines))
	}
	if lines[0] != "crates-io              crates.io" || lines[9] != "vscode-marketplace     VS Code" {
		t.Errorf("lines = %q", lines)
	}
}

func TestArgumentErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"--limit", "0", "x"},
		{"--limit", "many", "x"},
		{"--no-such-flag"},
		{"two", "ideas"},
		{"--completions", "elvish"},
		{"--cache-ttl=-1h", "x"},
		{"--cache-ttl", "an hour", "x"},
		{"--catalog-ttl=-24h", "x"},
		{"--catalog-ttl", "1d", "x"},
	} {
		_, stderr, code := runSilt(t, args...)
		if code != 2 || !strings.HasPrefix(stderr, "error: ") {
			t.Errorf("%q: exit %d, stderr %q", args, code, stderr)
		}
	}
}

func TestUnknownSourceExitsOneAndListsTheValidNames(t *testing.T) {
	_, stderr, code := runSilt(t, "--sources", "npm,nope", "cli tool that kills a process on a port")
	if code != 1 || !strings.Contains(stderr, "unknown source `nope`. Valid: crates-io, github,") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, stderr, code := runSilt(t, "--add", "codeberg,nope", "cli tool that kills a process on a port"); code != 1 || !strings.Contains(stderr, "unknown source `nope`.") {
		t.Errorf("--add: exit %d, stderr %q", code, stderr)
	}
	// As with clap's value_delimiter, an empty item is a name too.
	if _, stderr, code := runSilt(t, "--sources", "", "cli tool that kills a process on a port"); code != 1 || !strings.Contains(stderr, "unknown source ``") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestCompletionsAreGenerated(t *testing.T) {
	for _, shell := range shells {
		stdout, _, code := runSilt(t, "--completions", shell)
		if code != 0 || !strings.Contains(stdout, "silt") {
			t.Errorf("%s: exit %d, %d bytes", shell, code, len(stdout))
		}
	}
}

func TestAPIBaseWithoutModelIsAnError(t *testing.T) {
	_, stderr, code := runSilt(t, "--llm-verdict", "--api-base", "http://localhost:1/v1", "cli tool that kills a process on a port")
	if code != 1 || !strings.Contains(stderr, "api_base is set but no model was specified") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
