package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/freshness"
)

// UserAgent identifies silt to the registries, which should see who is
// calling. crates.io rejects requests without one.
func UserAgent() string {
	return "silt/" + silt.Version() + " (prior-art search; https://github.com/miku/silt)"
}

// NewHTTPClient returns the client shared by all adapters: a 10 s total
// timeout per request, 5 s to connect, and silt's User-Agent.
func NewHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = 5 * time.Second
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: userAgentTransport{base: tr, userAgent: UserAgent()},
	}
}

// userAgentTransport sets a default User-Agent on every request, like
// reqwest's ClientBuilder::user_agent.
type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", t.userAgent)
	}
	return t.base.RoundTrip(req)
}

// newRequest builds a request for rawURL with params as its query string.
func newRequest(ctx context.Context, method, rawURL string, params url.Values, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, &silt.HTTPError{Err: err}
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, &silt.HTTPError{Err: err}
	}
	return req, nil
}

// send performs req, reporting a failure to get any response as an
// HTTPError.
func send(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, &silt.HTTPError{Err: err}
	}
	return resp, nil
}

// decodeJSON closes resp.Body after checking for a 2xx status and decoding
// the whole body into v. Both failures are HTTPErrors, as they are for
// reqwest's error_for_status and json.
func decodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return err
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return &silt.HTTPError{StatusCode: resp.StatusCode, Err: err}
	}
	// Unmarshal, not a streaming Decoder: like serde_json, it rejects
	// trailing garbage after the first value.
	if err := json.Unmarshal(b, v); err != nil {
		return &silt.HTTPError{StatusCode: resp.StatusCode, Err: fmt.Errorf("error decoding response body: %w", err)}
	}
	return nil
}

// checkStatus turns a non-2xx response into an HTTPError, which is
// retryable.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &silt.HTTPError{
		StatusCode: resp.StatusCode,
		Err:        fmt.Errorf("HTTP status %s for url (%s)", resp.Status, resp.Request.URL),
	}
}

// get sends a GET request for rawURL with params.
func get(ctx context.Context, client *http.Client, rawURL string, params url.Values) (*http.Response, error) {
	req, err := newRequest(ctx, http.MethodGet, rawURL, params, nil)
	if err != nil {
		return nil, err
	}
	return send(client, req)
}

// getJSON sends a GET request for rawURL with params and decodes the JSON
// response into v.
func getJSON(ctx context.Context, client *http.Client, rawURL string, params url.Values, v any) error {
	resp, err := get(ctx, client, rawURL, params)
	if err != nil {
		return err
	}
	return decodeJSON(resp, v)
}

// readText closes resp.Body after checking for a 2xx status and reading the
// whole body.
func readText(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return "", err
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &silt.HTTPError{StatusCode: resp.StatusCode, Err: err}
	}
	return string(b), nil
}

// missingField is the error for a response that lacks a field patent's serde
// structs require. encoding/json leaves such fields at their zero value, so
// adapters decode them into pointers and check: a response that changed
// shape must fail its source ("not reached"), never pass for zero results.
func missingField(name string) error {
	return &silt.HTTPError{Err: fmt.Errorf("error decoding response body: missing field `%s`", name)}
}

// rfc3339Date normalises an RFC 3339 date field, or returns nil.
func rfc3339Date(l freshness.Lenient[string]) *string {
	if !l.OK {
		return nil
	}
	if s, ok := freshness.FromRFC3339(l.V); ok {
		return &s
	}
	return nil
}
