// Package httprec records HTTP exchanges and replays them, so that a search
// against the live registries can be repeated offline, with the same result
// every time.
//
// An exchange keeps only what silt's adapters look at: the method, URL and
// body of a request, and the status and body of its response, or the error
// it failed with. Request headers are left out, so a GITHUB_TOKEN never ends
// up in a recording.
package httprec

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// Exchange is a request and what came back for it.
type Exchange struct {
	Method      string `json:"method"`
	URL         string `json:"url"`
	RequestBody string `json:"request_body,omitempty"`
	Status      int    `json:"status,omitempty"`
	Body        string `json:"body,omitempty"`
	// Error is what the request failed with, if it got no response.
	Error string `json:"error,omitempty"`
}

func (ex Exchange) matches(req *http.Request, body string) bool {
	return ex.Method == req.Method && ex.URL == req.URL.String() && ex.RequestBody == body
}

// Recorder is an http.RoundTripper that sends each request through Transport
// and records the exchange.
type Recorder struct {
	Transport http.RoundTripper

	mu        sync.Mutex
	exchanges []Exchange
}

// RoundTrip implements http.RoundTripper. The response body is read in full
// before RoundTrip returns, so that it can be recorded.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := readBody(req)
	if err != nil {
		return nil, err
	}
	ex := Exchange{Method: req.Method, URL: req.URL.String(), RequestBody: body}

	out := req.Clone(req.Context())
	out.Body = io.NopCloser(strings.NewReader(body))
	resp, err := r.Transport.RoundTrip(out)
	if err == nil {
		var b []byte
		b, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(b))
		ex.Status, ex.Body = resp.StatusCode, string(b)
	}
	if err != nil {
		ex.Status, ex.Body, ex.Error = 0, "", err.Error()
		resp = nil
	}

	r.mu.Lock()
	r.exchanges = append(r.exchanges, ex)
	r.mu.Unlock()
	return resp, err
}

// Exchanges returns the recorded exchanges, sorted so that recording the same
// requests again gives the same list: concurrent requests are recorded in
// whatever order they finish. A request sent more than once, such as a
// retry, keeps the order it was sent in.
func (r *Recorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	exchanges := slices.Clone(r.exchanges)
	slices.SortStableFunc(exchanges, func(a, b Exchange) int {
		return cmp.Or(
			cmp.Compare(a.Method, b.Method),
			cmp.Compare(a.URL, b.URL),
			cmp.Compare(a.RequestBody, b.RequestBody),
		)
	})
	return exchanges
}

// Replayer is an http.RoundTripper that answers each request with a recorded
// exchange for the same method, URL and body, and never touches the network.
// Each exchange is used once, in the order recorded, so a request that failed
// and was retried replays the same way.
type Replayer struct {
	mu        sync.Mutex
	exchanges []Exchange // the ones not used yet
}

// NewReplayer returns a Replayer for exchanges.
func NewReplayer(exchanges []Exchange) *Replayer {
	return &Replayer{exchanges: slices.Clone(exchanges)}
}

// RoundTrip implements http.RoundTripper. A request with nothing recorded
// for it fails.
func (r *Replayer) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := readBody(req)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i := slices.IndexFunc(r.exchanges, func(ex Exchange) bool { return ex.matches(req, body) })
	if i < 0 {
		return nil, fmt.Errorf("httprec: nothing recorded for %s %s", req.Method, req.URL)
	}
	ex := r.exchanges[i]
	r.exchanges = slices.Delete(r.exchanges, i, i+1)
	if ex.Error != "" {
		return nil, errors.New(ex.Error)
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", ex.Status, http.StatusText(ex.Status)),
		StatusCode:    ex.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{},
		Body:          io.NopCloser(strings.NewReader(ex.Body)),
		ContentLength: int64(len(ex.Body)),
		Request:       req,
	}, nil
}

// readBody reads and closes the body of req, which a RoundTripper must close
// even when it fails.
func readBody(req *http.Request) (string, error) {
	if req.Body == nil {
		return "", nil
	}
	defer req.Body.Close()
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return "", fmt.Errorf("httprec: reading the request body: %w", err)
	}
	return string(b), nil
}

// Save writes exchanges to path as indented JSON. Bodies are kept as text,
// so a body that isn't UTF-8 is an error rather than a corrupted recording.
func Save(path string, exchanges []Exchange) error {
	for _, ex := range exchanges {
		if !utf8.ValidString(ex.RequestBody) || !utf8.ValidString(ex.Body) {
			return fmt.Errorf("httprec: %s %s: body is not UTF-8 text", ex.Method, ex.URL)
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep the HTML of scraped pages readable
	enc.SetIndent("", "  ")
	if err := enc.Encode(exchanges); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Load reads exchanges written by Save.
func Load(path string) ([]Exchange, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var exchanges []Exchange
	if err := json.Unmarshal(b, &exchanges); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return exchanges, nil
}
