package minilm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The model files, as fastembed downloads them for AllMiniLML6V2, pinned to a
// revision and checksummed.
const (
	Repo     = "Qdrant/all-MiniLM-L6-v2-onnx"
	Revision = "5f1b8cd78bc4fb444dd171e59b18f3a3af89a079"
)

// File is one file of the model.
type File struct {
	Name   string
	Size   int64
	SHA256 string
}

// Files are the files Load reads.
var Files = []File{
	{"model.onnx", 90387630, "bbd7b466f6d58e646fdc2bd5fd67b2f5e93c0b687011bd4548c420f7bd46f0c5"},
	{"tokenizer.json", 711661, "da0e79933b9ed51798a3ae27893d3c5fa4a201126cef75586296df9b4d2c62a0"},
}

// DefaultDir is where silt caches the model: a directory per revision under
// <UserCacheDir>/silt, so that the files download once for the whole machine.
func DefaultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "silt", "all-MiniLM-L6-v2-"+Revision[:7]), nil
}

// IsComplete reports whether dir holds every model file at its full size. A
// file is only ever renamed into place after its checksum matched, so a
// partial download never counts.
func IsComplete(dir string) bool {
	for _, f := range Files {
		fi, err := os.Stat(filepath.Join(dir, f.Name))
		if err != nil || fi.Size() != f.Size {
			return false
		}
	}
	return true
}

// Progress is told how much of a file has been downloaded.
type Progress func(file string, done, total int64)

// Download fetches the model files that dir lacks from the Hugging Face hub
// ($HF_ENDPOINT, by default https://huggingface.co), verifying each one's size
// and SHA-256 before renaming it into place.
func Download(ctx context.Context, dir, userAgent string, progress Progress) error {
	endpoint := strings.TrimSuffix(os.Getenv("HF_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = "https://huggingface.co"
	}
	// No overall timeout: 90 MB takes as long as it takes. A server that
	// doesn't answer, though, is given up on, and ctx cancels the rest.
	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}}
	return downloadFiles(ctx, client, endpoint, dir, userAgent, Files, progress)
}

func downloadFiles(ctx context.Context, client *http.Client, endpoint, dir, userAgent string, files []File, progress Progress) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		if fi, err := os.Stat(filepath.Join(dir, f.Name)); err == nil && fi.Size() == f.Size {
			continue
		}
		url := fmt.Sprintf("%s/%s/resolve/%s/%s", endpoint, Repo, Revision, f.Name)
		if err := download(ctx, client, url, userAgent, dir, f, progress); err != nil {
			return fmt.Errorf("downloading %s: %w", f.Name, err)
		}
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url, userAgent, dir string, f File, progress Progress) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(dir, f.Name+".*.part")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	h := sha256.New()
	var body io.Reader = resp.Body
	if progress != nil {
		body = &progressReader{r: body, file: f.Name, total: f.Size, report: progress}
	}
	// One byte over the expected size is enough to tell it's wrong.
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(body, f.Size+1))
	if err == nil {
		err = tmp.Sync()
	}
	if err == nil {
		err = tmp.Chmod(0o644) // CreateTemp makes it 0600
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != f.Size {
		return fmt.Errorf("got %d bytes, want %d", n, f.Size)
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != f.SHA256 {
		return fmt.Errorf("SHA-256 is %s, want %s", sum, f.SHA256)
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, f.Name))
}

type progressReader struct {
	r           io.Reader
	file        string
	done, total int64
	report      Progress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.report(p.file, p.done, p.total)
	return n, err
}
