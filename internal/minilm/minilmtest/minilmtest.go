// Package minilmtest finds the embedding model for tests, which must never
// download it.
package minilmtest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/miku/silt/internal/minilm"
)

// Dir returns a directory holding the model files: $SILT_MINILM_DIR, silt's
// cache, or the copy fastembed downloaded for patent. Without one the test is
// skipped; go run ./scripts/fetchmodel downloads the model into silt's cache.
func Dir(t testing.TB) string {
	t.Helper()
	if dir := find(); dir != "" {
		return dir
	}
	t.Skip("no all-MiniLM-L6-v2 model; run go run ./scripts/fetchmodel or set SILT_MINILM_DIR")
	return ""
}

func find() string {
	if dir := os.Getenv("SILT_MINILM_DIR"); dir != "" {
		return dir
	}
	if dir, err := minilm.DefaultDir(); err == nil && minilm.IsComplete(dir) {
		return dir
	}
	home, _ := os.UserHomeDir()
	dirs, _ := filepath.Glob(filepath.Join(home, ".cache/patent/fastembed/models--Qdrant--all-MiniLM-L6-v2-onnx/snapshots/*"))
	for _, dir := range dirs {
		if minilm.IsComplete(dir) {
			return dir
		}
	}
	return ""
}

var load = sync.OnceValues(func() (*minilm.Model, error) { return minilm.Load(find()) })

// Model returns the model, loaded once per test binary.
func Model(t testing.TB) *minilm.Model {
	t.Helper()
	Dir(t)
	m, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return m
}
