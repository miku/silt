package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miku/silt/internal/tui"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func TestLimitValue(t *testing.T) {
	var l limitValue
	for _, ok := range []string{"1", "50", "4294967295"} {
		if err := l.Set(ok); err != nil {
			t.Errorf("Set(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "-1", "4294967296", "five", ""} {
		if err := l.Set(bad); err == nil {
			t.Errorf("Set(%q) succeeded", bad)
		}
	}
}

func TestTTLValue(t *testing.T) {
	var d ttlValue
	for s, want := range map[string]time.Duration{"0": 0, "30m": 30 * time.Minute, "24h": 24 * time.Hour, "1h30m": 90 * time.Minute} {
		if err := d.Set(s); err != nil || time.Duration(d) != want {
			t.Errorf("Set(%q) = %v, %v; want %v", s, err, time.Duration(d), want)
		}
	}
	for _, bad := range []string{"-1h", "1", "an hour", ""} {
		if err := d.Set(bad); err == nil {
			t.Errorf("Set(%q) succeeded", bad)
		}
	}
	if d = ttlValue(time.Hour); d.String() != "1h" {
		t.Errorf("String() = %q, want 1h", d.String())
	}
}

func TestResolveTTL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flagSet bool
		flag    time.Duration
		env     map[string]string
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: time.Hour},
		{name: "env", env: map[string]string{"SILT_CACHE_TTL": "10m"}, want: 10 * time.Minute},
		{name: "env turns it off", env: map[string]string{"SILT_CACHE_TTL": "0"}, want: 0},
		{name: "flag beats env", flagSet: true, flag: 0, env: map[string]string{"SILT_CACHE_TTL": "10m"}, want: 0},
		{name: "invalid env", env: map[string]string{"SILT_CACHE_TTL": "soon"}, wantErr: true},
		{name: "negative env", env: map[string]string{"SILT_CACHE_TTL": "-5m"}, wantErr: true},
		{name: "set but empty env", env: map[string]string{"SILT_CACHE_TTL": ""}, wantErr: true},
	} {
		got, err := resolveTTL(tc.flagSet, tc.flag, "SILT_CACHE_TTL", time.Hour, env(tc.env))
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s: %v, %v; want %v (error: %v)", tc.name, got, err, tc.want, tc.wantErr)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "SILT_CACHE_TTL: ") {
			t.Errorf("%s: the error should name the variable: %v", tc.name, err)
		}
	}
	// Each TTL reads its own variable.
	both := env(map[string]string{"SILT_CACHE_TTL": "0", "SILT_CATALOG_TTL": "6h"})
	if got, err := resolveTTL(false, 0, "SILT_CATALOG_TTL", 24*time.Hour, both); err != nil || got != 6*time.Hour {
		t.Errorf("SILT_CATALOG_TTL: %v, %v; want 6h", got, err)
	}
	if got, err := resolveTTL(false, 0, "SILT_CATALOG_TTL", 24*time.Hour, env(map[string]string{"SILT_CACHE_TTL": "0"})); err != nil || got != 24*time.Hour {
		t.Errorf("SILT_CACHE_TTL changed the catalog TTL: %v, %v", got, err)
	}
}

func TestABrowserPathThatIsNoFileIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "chrome")
	for name, value := range map[string]string{"SILT_BROWSER": missing, "CLOAKBROWSER_BINARY_PATH": missing} {
		var stderr strings.Builder
		var opened []tui.Options
		e := tuiEnv(&stderr, &opened, nil)
		e.lookupEnv = env(map[string]string{name: value})
		if code := run(nil, e); code != 1 || !strings.HasPrefix(stderr.String(), "Error: "+name+": ") || len(opened) != 0 {
			t.Errorf("%s: exit %d, stderr %q", name, code, stderr.String())
		}
	}
	var opened []tui.Options
	e := tuiEnv(io.Discard, &opened, nil)
	e.lookupEnv = env(map[string]string{"SILT_BROWSER": "off", "CLOAKBROWSER_BINARY_PATH": missing})
	if code := run(nil, e); code != 0 || len(opened) != 1 {
		t.Errorf("SILT_BROWSER=off: exit %d; it should win over a broken CLOAKBROWSER_BINARY_PATH", code)
	}
}

func TestListValueSplitsOnEveryCommaAndAccumulates(t *testing.T) {
	var l listValue
	for _, s := range []string{"npm,crates-io", "github", ",x,"} {
		if err := l.Set(s); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"npm", "crates-io", "github", "", "x", ""}; !reflect.DeepEqual([]string(l), want) {
		t.Errorf("list = %q, want %q", l, want)
	}
}

// tuiEnv is an environment on a terminal whose TUI records its options and
// returns err.
func tuiEnv(stderr io.Writer, opened *[]tui.Options, err error) environment {
	return environment{
		stdout:     io.Discard,
		stderr:     stderr,
		isTerminal: true,
		lookupEnv:  env(nil),
		runTUI: func(_ context.Context, o tui.Options) error {
			*opened = append(*opened, o)
			return err
		},
	}
}

func TestNoIdeaOnATerminalOpensTheInteractiveTUI(t *testing.T) {
	var opened []tui.Options
	if code := run(nil, tuiEnv(io.Discard, &opened, nil)); code != 0 {
		t.Errorf("exit %d", code)
	}
	if len(opened) != 1 || opened[0].Result != nil || opened[0].Search == nil {
		t.Errorf("opened %+v", opened)
	}

	var stderr strings.Builder
	if code := run(nil, tuiEnv(&stderr, &opened, errors.New("no tty"))); code != 1 || stderr.String() != "Error: no tty\n" {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
	// --json still needs an idea, on a terminal too.
	if code := run([]string{"--json"}, tuiEnv(io.Discard, &opened, nil)); code != 1 || len(opened) != 2 {
		t.Errorf("exit %d, %d TUIs", code, len(opened))
	}
}

func TestDownloadProgressDrawsOneLinePerFile(t *testing.T) {
	var b strings.Builder
	p := downloadProgress(&b)
	for _, done := range []int64{0, 1, 2, 500_000, 500_001, 1_000_000} {
		p("model.onnx", done, 1_000_000)
	}
	p("tokenizer.json", 10, 10)
	want := "\r   model.onnx:   0% of 1.0 MB" +
		"\r   model.onnx:  50% of 1.0 MB" +
		"\r   model.onnx: 100% of 1.0 MB\n" +
		"\r   tokenizer.json: 100% of 0.0 MB\n"
	if b.String() != want {
		t.Errorf("progress = %q\nwant       %q", b.String(), want)
	}
}
