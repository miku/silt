package sources

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/miku/silt/model"
)

// mockServer is a stand-in registry that records the requests it serves.
type mockServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func newMockServer(t *testing.T, h http.Handler) *mockServer {
	t.Helper()
	m := &mockServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.requests = append(m.requests, r)
		m.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *mockServer) Requests() []*http.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*http.Request(nil), m.requests...)
}

// respond serves body with status, as JSON.
func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}
}

// onPath serves h on path only; anything else is a 404, as an unmatched
// wiremock request is.
func onPath(path string, h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	}
}

// byParam routes on the value of the query parameter name, with a 404 for
// any value not in routes.
func byParam(name string, routes map[string]http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.URL.Query().Get(name)]; ok {
			h.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

// query has keywords that join to the string asserted on the wire.
func query() model.Query {
	return model.Query{Idea: "a fast async runtime for rust", Keywords: []string{"async", "runtime"}}
}

// narrowingQuery uses distinct keyword lengths throughout (ide=3, code=4,
// spell=5, syntax=6, mistakes=8), so the "longest 3" and "longest 2"
// selections are unambiguous.
func narrowingQuery() model.Query {
	return model.Query{
		Idea:     "an ide plugin that highlights spelling mistakes in code comments",
		Keywords: []string{"ide", "code", "spell", "syntax", "mistakes"},
	}
}

func ptr[T any](v T) *T { return &v }

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
