package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deref(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestParsesAllFields(t *testing.T) {
	cfg, err := Parse([]byte(`
		model    = "qwen3.5-122b"
		api_base = "http://localhost:1234/v1"
		api_key  = "test-key"
	`))
	if err != nil {
		t.Fatal(err)
	}
	if deref(cfg.Model) != "qwen3.5-122b" || deref(cfg.APIBase) != "http://localhost:1234/v1" || deref(cfg.APIKey) != "test-key" {
		t.Errorf("cfg = %v %v %v", deref(cfg.Model), deref(cfg.APIBase), deref(cfg.APIKey))
	}
}

func TestEmptyFileIsOK(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil || cfg.Model != nil || cfg.APIBase != nil || cfg.APIKey != nil {
		t.Errorf("Parse(\"\") = %+v, %v", cfg, err)
	}
}

func TestPartialFieldsOK(t *testing.T) {
	cfg, err := Parse([]byte(`model = "qwen2.5:3b"`))
	if err != nil || deref(cfg.Model) != "qwen2.5:3b" || cfg.APIBase != nil {
		t.Errorf("Parse = %+v, %v", cfg, err)
	}
}

func TestUnknownFieldIsError(t *testing.T) {
	_, err := Parse([]byte(`typo_key = "oops"`))
	if err == nil || !strings.Contains(err.Error(), "unknown field `typo_key`") {
		t.Errorf("err = %v", err)
	}
}

func TestEmptyStringIsSetNotMissing(t *testing.T) {
	cfg, err := Parse([]byte(`api_key = ""`))
	if err != nil || cfg.APIKey == nil || *cfg.APIKey != "" {
		t.Errorf("Parse = %+v, %v", cfg, err)
	}
}

func TestWrongTypeIsError(t *testing.T) {
	if _, err := Parse([]byte(`model = 5`)); err == nil {
		t.Error("a number for model must be an error")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // os.UserConfigDir on Linux
	t.Setenv("HOME", dir)            // ... and on macOS
	t.Setenv("AppData", dir)         // ... and on Windows

	// A missing file is fine.
	if cfg, err := Load(); err != nil || cfg.Model != nil {
		t.Fatalf("Load without a file = %+v, %v", cfg, err)
	}

	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`model = "m"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(); err != nil || deref(cfg.Model) != "m" {
		t.Errorf("Load = %+v, %v", cfg, err)
	}

	// An unknown key is an error that names the file.
	if err := os.WriteFile(path, []byte("model = \"m\"\napi_bsae = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.HasPrefix(err.Error(), path+": ") || !strings.Contains(err.Error(), "`api_bsae`") {
		t.Errorf("err = %v", err)
	}
}
