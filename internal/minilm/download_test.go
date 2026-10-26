package minilm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeHub serves files under /<Repo>/resolve/<Revision>/ and counts the
// requests.
func fakeHub(t *testing.T, files map[string]string) (*httptest.Server, *atomic.Int32) {
	var requests atomic.Int32
	prefix := "/" + Repo + "/resolve/" + Revision + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, ok := files[strings.TrimPrefix(r.URL.Path, prefix)]
		if !ok || !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func fileFor(name, content string) File {
	sum := sha256.Sum256([]byte(content))
	return File{Name: name, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}

func TestDownloadFetchesVerifiesAndSkipsWhatIsThere(t *testing.T) {
	content := map[string]string{"a.onnx": strings.Repeat("weights", 1000), "b.json": `{"vocab": {}}`}
	files := []File{fileFor("a.onnx", content["a.onnx"]), fileFor("b.json", content["b.json"])}
	srv, requests := fakeHub(t, content)
	dir := filepath.Join(t.TempDir(), "model")

	var reported int64
	progress := func(file string, done, total int64) {
		if file == "a.onnx" {
			reported = done
		}
	}
	if err := downloadFiles(context.Background(), srv.Client(), srv.URL, dir, "silt-test", files, progress); err != nil {
		t.Fatal(err)
	}
	for name, want := range content {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
	if reported != files[0].Size {
		t.Errorf("progress reported %d of %d bytes", reported, files[0].Size)
	}
	if got := entries(t, dir); len(got) != 2 {
		t.Errorf("left behind %v", got)
	}

	// Complete files are not fetched again.
	if err := downloadFiles(context.Background(), srv.Client(), srv.URL, dir, "silt-test", files, nil); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
}

func TestDownloadRejectsWhatDoesNotVerify(t *testing.T) {
	good := fileFor("m.onnx", "the real weights")
	for name, served := range map[string]string{
		"wrong checksum": "the fake weights", // same length
		"short":          "the real",
		"long":           "the real weights, and more",
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := fakeHub(t, map[string]string{"m.onnx": served})
			dir := t.TempDir()
			if err := downloadFiles(context.Background(), srv.Client(), srv.URL, dir, "silt-test", []File{good}, nil); err == nil {
				t.Fatal("want an error")
			}
			// Neither the file nor its temporary part is left behind.
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("left behind %v", got)
			}
		})
	}
}

func TestDownloadReportsHTTPErrors(t *testing.T) {
	srv, _ := fakeHub(t, nil)
	err := downloadFiles(context.Background(), srv.Client(), srv.URL, t.TempDir(), "silt-test", []File{fileFor("m.onnx", "x")}, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}

func TestIsCompleteChecksEveryFileAtFullSize(t *testing.T) {
	dir := t.TempDir()
	if IsComplete(dir) {
		t.Error("an empty directory is not complete")
	}
	for _, f := range Files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if IsComplete(dir) {
		t.Error("empty files are not complete")
	}
}

// TestPinnedChecksumsMatchPatentsFiles checks the pins against the files
// fastembed downloaded for patent, when they are there.
func TestPinnedChecksumsMatchPatentsFiles(t *testing.T) {
	dir := patentModelDir()
	if dir == "" {
		t.Skip("no patent model cache")
	}
	for _, f := range Files {
		b, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if int64(len(b)) != f.Size || hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Errorf("%s: %d bytes, SHA-256 %x; pinned %d, %s", f.Name, len(b), sum, f.Size, f.SHA256)
		}
	}
}
