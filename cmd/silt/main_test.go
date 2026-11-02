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

	"github.com/miku/silt/internal/config"
	"github.com/miku/silt/internal/tui"
)

func str(s string) *string { return &s }

func deref(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func env(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func TestResolveBackendPrecedence(t *testing.T) {
	cfg := config.Config{Model: str("cfg-model"), APIBase: str("http://cfg/v1"), APIKey: str("cfg-key")}

	// Flag (or its env var) beats the config file.
	b, _, err := resolveBackend(str("http://flag/v1"), str("flag-key"), str("flag-model"), true, cfg, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if deref(b.apiBase) != "http://flag/v1" || deref(b.apiKey) != "flag-key" || deref(b.model) != "flag-model" {
		t.Errorf("flags: %v %v %v", deref(b.apiBase), deref(b.apiKey), deref(b.model))
	}

	// The config file beats the defaults, and OPENAI_API_KEY comes last.
	b, _, _ = resolveBackend(nil, nil, nil, true, cfg, env(map[string]string{"OPENAI_API_KEY": "openai"}))
	if deref(b.apiBase) != "http://cfg/v1" || deref(b.apiKey) != "cfg-key" || deref(b.model) != "cfg-model" {
		t.Errorf("config: %v %v %v", deref(b.apiBase), deref(b.apiKey), deref(b.model))
	}
	b, _, _ = resolveBackend(nil, nil, nil, true, config.Config{}, env(map[string]string{"OPENAI_API_KEY": "openai"}))
	if deref(b.apiKey) != "openai" || b.apiBase != nil || b.model != nil || b.ollamaEndpoint != nil {
		t.Errorf("defaults: %v %v %v %v", deref(b.apiBase), deref(b.apiKey), deref(b.model), deref(b.ollamaEndpoint))
	}
}

func TestResolveBackendReadsOLLAMA_HOST(t *testing.T) {
	b, notes, err := resolveBackend(nil, nil, nil, true, config.Config{}, env(map[string]string{"OLLAMA_HOST": "strix.tiny.vpn"}))
	if err != nil || len(notes) != 0 || deref(b.ollamaEndpoint) != "http://strix.tiny.vpn:11434" {
		t.Errorf("endpoint %v, notes %q, err %v", deref(b.ollamaEndpoint), notes, err)
	}
}

func TestResolveBackendWarnings(t *testing.T) {
	for _, tc := range []struct {
		name             string
		base, key, model *string
		fast             bool
		cfg              config.Config
		openAIKey        bool
		want             []string
		wantErr          bool
	}{
		{name: "nothing set"},
		{name: "base without model", base: str("http://x/v1"), wantErr: true},
		{name: "base without model, but --fast skips the LLM", base: str("http://x/v1"), fast: true,
			want: []string{"warning: --fast skips the LLM, so api_base has no effect."}},
		{name: "explicit key without base", key: str("k"),
			want: []string{"warning: api_key has no effect without api_base; using local Ollama."}},
		{name: "key from the config file without base", cfg: config.Config{APIKey: str("k")},
			want: []string{"warning: api_key has no effect without api_base; using local Ollama."}},
		// OPENAI_API_KEY is only a fallback: having it in the environment is
		// no reason to warn.
		{name: "OPENAI_API_KEY without base", openAIKey: true},
		{name: "base and model", base: str("http://x/v1"), model: str("m"),
			want: []string{"note: --api-base sends your search query to http://x/v1 to generate the verdict; " +
				"the default local Ollama backend and --fast keep it on your machine."}},
	} {
		vars := map[string]string{}
		if tc.openAIKey {
			vars["OPENAI_API_KEY"] = "sk"
		}
		_, notes, err := resolveBackend(tc.base, tc.key, tc.model, tc.fast, tc.cfg, env(vars))
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if len(notes) != len(tc.want) || (len(notes) > 0 && !reflect.DeepEqual(notes, tc.want)) {
			t.Errorf("%s: notes = %q, want %q", tc.name, notes, tc.want)
		}
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
		loadConfig: func() (config.Config, error) { return config.Config{}, nil },
		runTUI: func(_ context.Context, o tui.Options) error {
			*opened = append(*opened, o)
			return err
		},
	}
}

func TestNoIdeaOnATerminalOpensTheInteractiveTUI(t *testing.T) {
	var opened []tui.Options
	if code := run([]string{"--model", "llama3"}, tuiEnv(io.Discard, &opened, nil)); code != 0 {
		t.Errorf("exit %d", code)
	}
	if len(opened) != 1 || opened[0].Result != nil || opened[0].Search == nil ||
		opened[0].Backend != "Verdicts by llama3 on the local Ollama." {
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

func TestBackendNote(t *testing.T) {
	for _, tc := range []struct {
		b    backend
		fast bool
		want string
	}{
		{backend{}, false, "Verdicts by qwen3.5 on the local Ollama."},
		{backend{model: str("llama3")}, false, "Verdicts by llama3 on the local Ollama."},
		{backend{ollamaEndpoint: str("http://strix.tiny.vpn:11434")}, false,
			"Verdicts by qwen3.5 on the Ollama at http://strix.tiny.vpn:11434."},
		// --api-base wins over OLLAMA_HOST.
		{backend{apiBase: str("https://api.openai.com/v1"), model: str("gpt-4o-mini"), ollamaEndpoint: str("http://strix.tiny.vpn:11434")}, false,
			"Verdicts by gpt-4o-mini, through the API at https://api.openai.com/v1."},
		{backend{apiBase: str("https://api.openai.com/v1"), model: str("gpt-4o-mini")}, false,
			"Verdicts by gpt-4o-mini, through the API at https://api.openai.com/v1."},
		{backend{apiBase: str("https://api.openai.com/v1"), model: str("gpt-4o-mini")}, true,
			"--fast: verdicts come from the similarity data, without an LLM."},
	} {
		if got := backendNote(tc.b, tc.fast); got != tc.want {
			t.Errorf("backendNote(%+v, %v) = %q", tc.b, tc.fast, got)
		}
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
