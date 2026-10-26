package main

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

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
	b, _, err := resolveBackend(str("http://flag/v1"), str("flag-key"), str("flag-model"), false, cfg, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if deref(b.apiBase) != "http://flag/v1" || deref(b.apiKey) != "flag-key" || deref(b.model) != "flag-model" {
		t.Errorf("flags: %v %v %v", deref(b.apiBase), deref(b.apiKey), deref(b.model))
	}

	// The config file beats the defaults, and OPENAI_API_KEY comes last.
	b, _, _ = resolveBackend(nil, nil, nil, false, cfg, env(map[string]string{"OPENAI_API_KEY": "openai"}))
	if deref(b.apiBase) != "http://cfg/v1" || deref(b.apiKey) != "cfg-key" || deref(b.model) != "cfg-model" {
		t.Errorf("config: %v %v %v", deref(b.apiBase), deref(b.apiKey), deref(b.model))
	}
	b, _, _ = resolveBackend(nil, nil, nil, false, config.Config{}, env(map[string]string{"OPENAI_API_KEY": "openai"}))
	if deref(b.apiKey) != "openai" || b.apiBase != nil || b.model != nil {
		t.Errorf("defaults: %v %v %v", deref(b.apiBase), deref(b.apiKey), deref(b.model))
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
