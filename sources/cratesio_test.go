package sources

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/miku/silt/model"
)

const twoCrateBody = `{
  "crates": [
    {
      "name": "tokio",
      "updated_at": "2026-05-15T06:13:41.215606Z",
      "description": "An event-driven, non-blocking I/O platform.",
      "downloads": 950000000,
      "max_version": "1.40.0",
      "repository": "https://github.com/tokio-rs/tokio"
    },
    {
      "name": "async-std",
      "description": "Async version of the Rust standard library.",
      "downloads": 45000000,
      "max_version": "1.13.0",
      "repository": "https://github.com/async-rs/async-std"
    }
  ],
  "meta": { "total": 2 }
}`

func cratesIOFor(srv *mockServer) *CratesIO {
	return &CratesIO{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func cratesIOServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/api/v1/crates", respond(status, body)))
}

func TestCratesIOIDIsCratesIO(t *testing.T) {
	if got := NewCratesIO(nil).ID(); got != model.CratesIO {
		t.Fatalf("ID() = %v", got)
	}
}

func TestCratesIOMapsResultsIntoMatches(t *testing.T) {
	srv := cratesIOServer(t, 200, twoCrateBody)
	matches, err := cratesIOFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	tokio := matches[0]
	if tokio.Name != "tokio" || tokio.Source != model.CratesIO ||
		tokio.Description != "An event-driven, non-blocking I/O platform." ||
		deref(tokio.Popularity) != uint64(950_000_000) {
		t.Errorf("tokio = %+v", tokio)
	}
	// Similarity is filled in later by ranking; sources leave it at 0.
	if tokio.Similarity != 0 {
		t.Errorf("similarity = %v, want 0", tokio.Similarity)
	}
	if m := matches[1]; m.Name != "async-std" || deref(m.Popularity) != uint64(45_000_000) {
		t.Errorf("async-std = %+v", m)
	}
}

func TestCratesIOLinksUseTheConfiguredBaseURL(t *testing.T) {
	// A request served by a mock server (or a crates.io mirror) surfaces
	// links against the same host we queried, not the production host.
	srv := cratesIOServer(t, 200, twoCrateBody)
	matches, err := cratesIOFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := matches[0].URL, srv.URL+"/crates/tokio"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if got, want := matches[1].URL, srv.URL+"/crates/async-std"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
}

func TestCratesIOSendsJoinedKeywordsAsQueryAndAUserAgent(t *testing.T) {
	srv := cratesIOServer(t, 200, twoCrateBody)
	if _, err := cratesIOFor(srv).Search(context.Background(), query()); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	if got := reqs[0].URL.Query().Get("q"); got != "async runtime" {
		t.Errorf("q = %q, want the space-joined keywords", got)
	}
	if got := reqs[0].URL.Query().Get("per_page"); got != "20" {
		t.Errorf("per_page = %q", got)
	}
	// crates.io rejects requests without a User-Agent.
	if reqs[0].UserAgent() == "" {
		t.Error("no User-Agent sent")
	}
}

func TestCratesIOUserAgentContainsCorrectGitHubHandle(t *testing.T) {
	srv := cratesIOServer(t, 200, twoCrateBody)
	if _, err := cratesIOFor(srv).Search(context.Background(), query()); err != nil {
		t.Fatal(err)
	}
	if ua := srv.Requests()[0].UserAgent(); !strings.Contains(ua, "miku/silt") {
		t.Errorf("expected 'miku/silt' in User-Agent, got %q", ua)
	}
}

func TestCratesIONullDescriptionBecomesEmptyString(t *testing.T) {
	srv := cratesIOServer(t, 200, `{"crates": [{"name": "mystery-crate", "description": null, "downloads": 7}], "meta": {"total": 1}}`)
	matches, err := cratesIOFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Description != "" {
		t.Errorf("matches = %+v", matches)
	}
}

func TestCratesIOMissingDownloadsBecomesNone(t *testing.T) {
	srv := cratesIOServer(t, 200, `{"crates": [{"name": "obscure", "description": "no download count here"}], "meta": {"total": 1}}`)
	matches, err := cratesIOFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Popularity != nil {
		t.Errorf("matches = %+v", matches)
	}
}

func TestCratesIOEmptyResultsIsOKNotError(t *testing.T) {
	srv := cratesIOServer(t, 200, `{"crates": [], "meta": {"total": 0}}`)
	matches, err := cratesIOFor(srv).Search(context.Background(), query())
	if err != nil || len(matches) != 0 {
		t.Errorf("Search = %v, %v; want no matches and no error", matches, err)
	}
}

func TestCratesIOServerErrorIsPropagated(t *testing.T) {
	srv := cratesIOServer(t, 500, "")
	if _, err := cratesIOFor(srv).Search(context.Background(), query()); err == nil {
		t.Error("a 5xx response must surface as an error")
	}
}

func TestCratesIOMalformedBodyIsPropagated(t *testing.T) {
	srv := cratesIOServer(t, 200, "not json at all")
	if _, err := cratesIOFor(srv).Search(context.Background(), query()); err == nil {
		t.Error("an unparseable body must surface as an error")
	}
}

func TestCratesIOChangedResponseShapeIsAnErrorNotZeroResults(t *testing.T) {
	// patent's serde structs require these fields; a response without them
	// is drift and must fail the source rather than read as "no prior art".
	for _, body := range []string{
		`{"meta": {"total": 0}}`,
		`{"crates": null}`,
		`{"crates": [{"description": "nameless"}]}`,
		`{"crates": []} trailing`,
	} {
		srv := cratesIOServer(t, 200, body)
		if _, err := cratesIOFor(srv).Search(context.Background(), query()); err == nil {
			t.Errorf("%s: want an error", body)
		}
	}
}

func TestCratesIONormalisesLastUpdatedAndOmitsItWhenAbsent(t *testing.T) {
	srv := cratesIOServer(t, 200, twoCrateBody)
	m, err := cratesIOFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if got := deref(m[0].LastUpdated); got != "2026-05-15T06:13:41Z" {
		t.Errorf("last_updated = %v", got)
	}
	if m[1].LastUpdated != nil {
		t.Error("no updated_at in the fixture, so last_updated must be nil")
	}
}

func TestCratesIOMalformedDateNeverFailsTheSource(t *testing.T) {
	for _, date := range []string{`"last tuesday"`, `1778477360`, `{"iso": "2026-03-24"}`} {
		body := strings.Replace(twoCrateBody, `"2026-05-15T06:13:41.215606Z"`, date, 1)
		srv := cratesIOServer(t, 200, body)
		m, err := cratesIOFor(srv).Search(context.Background(), query())
		if err != nil {
			t.Errorf("updated_at %s failed the source: %v", date, err)
			continue
		}
		if len(m) != 2 || m[0].LastUpdated != nil {
			t.Errorf("updated_at %s: matches = %+v", date, m)
		}
	}
}

func TestCratesIONarrowsWhenTheFullQueryIsEmpty(t *testing.T) {
	srv := newMockServer(t, onPath("/api/v1/crates", byParam("q", map[string]http.Handler{
		"ide code spell syntax mistakes": respond(200, `{"crates": []}`),
		"spell syntax mistakes":          respond(200, twoCrateBody),
	})))
	matches, err := cratesIOFor(srv).Search(context.Background(), narrowingQuery())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0].Name != "tokio" {
		t.Errorf("matches = %+v", matches)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("got %d requests, want 2", n)
	}
}
