package httprec

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type reply struct {
	status int
	body   string
}

func do(t *testing.T, client *http.Client, method, url, body string) (reply, error) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := client.Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{resp.StatusCode, string(b)}, nil
}

// registry answers the first request with a 500 and every later one with
// what it was asked: the method, the path and query, and the request body.
func registry(t *testing.T) *httptest.Server {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			http.Error(w, "busy", http.StatusInternalServerError)
			return
		}
		b, _ := io.ReadAll(r.Body)
		io.WriteString(w, r.Method+" "+r.URL.RequestURI()+" "+string(b))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReplayAnswersAsTheRecordingDid(t *testing.T) {
	srv := registry(t)
	requests := []struct{ method, url, body string }{
		{"GET", srv.URL + "/search?q=port", ""}, // 500
		{"GET", srv.URL + "/search?q=port", ""}, // the retry
		{"GET", srv.URL + "/search?q=kill", ""},
		{"POST", srv.URL + "/query", `{"q":"port"}`},
		{"POST", srv.URL + "/query", `{"q":"kill"}`},
	}

	rec := &Recorder{Transport: http.DefaultTransport}
	var live []reply
	for _, r := range requests {
		got, err := do(t, &http.Client{Transport: rec}, r.method, r.url, r.body)
		if err != nil {
			t.Fatal(err)
		}
		live = append(live, got)
	}
	if live[0].status != 500 || live[1].status != 200 {
		t.Fatalf("live = %v, want a 500 and then a 200", live[:2])
	}

	path := filepath.Join(t.TempDir(), "exchanges.json")
	if err := Save(path, rec.Exchanges()); err != nil {
		t.Fatal(err)
	}
	exchanges, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close() // the replay must not need it

	client := &http.Client{Transport: NewReplayer(exchanges)}
	for i, r := range requests {
		got, err := do(t, client, r.method, r.url, r.body)
		if err != nil {
			t.Fatalf("replaying %v: %v", r, err)
		}
		if got != live[i] {
			t.Errorf("replaying %v = %v, want %v", r, got, live[i])
		}
	}
}

func TestReplayFailsARequestThatWasNotRecorded(t *testing.T) {
	client := &http.Client{Transport: NewReplayer([]Exchange{
		{Method: "GET", URL: "http://registry.test/search?q=port", Status: 200, Body: "[]"},
	})}
	for _, r := range []struct{ method, url, body string }{
		{"GET", "http://registry.test/search?q=kill", ""},
		{"POST", "http://registry.test/search?q=port", ""},
		{"GET", "http://registry.test/search?q=port", "{}"},
	} {
		if _, err := do(t, client, r.method, r.url, r.body); err == nil {
			t.Errorf("replaying %v: want an error", r)
		}
	}
	// Each exchange answers once.
	if _, err := do(t, client, "GET", "http://registry.test/search?q=port", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := do(t, client, "GET", "http://registry.test/search?q=port", ""); err == nil {
		t.Error("an exchange answered twice")
	}
}

func TestReplayFailsWhereTheRecordingFailed(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // connection refused

	rec := &Recorder{Transport: http.DefaultTransport}
	if _, err := do(t, &http.Client{Transport: rec}, "GET", srv.URL, ""); err == nil {
		t.Fatal("want an error from a closed server")
	}
	exchanges := rec.Exchanges()
	if len(exchanges) != 1 || !strings.Contains(exchanges[0].Error, "connection refused") {
		t.Fatalf("recorded %+v, want the connection error", exchanges)
	}
	_, err := do(t, &http.Client{Transport: NewReplayer(exchanges)}, "GET", srv.URL, "")
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("replay error = %v, want the recorded connection error", err)
	}
}

func TestRecordingLeavesOutRequestHeaders(t *testing.T) {
	srv := registry(t)
	rec := &Recorder{Transport: http.DefaultTransport}
	if _, err := do(t, &http.Client{Transport: rec}, "GET", srv.URL, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "exchanges.json")
	if err := Save(path, rec.Exchanges()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatalf("the Authorization header was recorded:\n%s", b)
	}
}

func TestExchangesAreSortedSoRecordingsAreStable(t *testing.T) {
	rec := &Recorder{exchanges: []Exchange{
		{Method: "GET", URL: "http://b.test/", Status: 200},
		{Method: "GET", URL: "http://a.test/", Status: 500},
		{Method: "GET", URL: "http://a.test/", Status: 200},
		{Method: "POST", URL: "http://a.test/", RequestBody: "{}"},
	}}
	want := []Exchange{
		{Method: "GET", URL: "http://a.test/", Status: 500},
		{Method: "GET", URL: "http://a.test/", Status: 200},
		{Method: "GET", URL: "http://b.test/", Status: 200},
		{Method: "POST", URL: "http://a.test/", RequestBody: "{}"},
	}
	if got := rec.Exchanges(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Exchanges() = %+v, want %+v", got, want)
	}
}

func TestSaveRefusesBodiesThatAreNotText(t *testing.T) {
	err := Save(filepath.Join(t.TempDir(), "exchanges.json"), []Exchange{
		{Method: "GET", URL: "http://a.test/", Status: 200, Body: "\xff\xfe"},
	})
	if err == nil {
		t.Fatal("want an error for a body that isn't UTF-8")
	}
}
