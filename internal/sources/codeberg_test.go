package sources

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miku/silt/internal/model"
)

// Trimmed from a live response: a repository whose description runs over
// two lines, and one without a description.
const codebergBody = `{"ok": true, "data": [
  {"id": 1, "full_name": "ctfrancia/gkp", "description": "GKP (Go Kill Port)\n\nkills processes on a port",
   "html_url": "https://codeberg.org/ctfrancia/gkp", "stars_count": 3, "fork": false, "mirror": false,
   "updated_at": "2025-12-08T10:30:00+01:00"},
  {"id": 2, "full_name": "someone/scratch", "description": "", "html_url": "https://codeberg.org/someone/scratch",
   "stars_count": 0, "updated_at": "2025-01-01T00:00:00Z"}
]}`

func codebergFor(srv *mockServer) *Codeberg {
	return &Codeberg{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func codebergServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/api/v1/repos/search", respond(status, body)))
}

func TestCodebergIDIsCodeberg(t *testing.T) {
	if got := NewCodeberg(nil).ID(); got != model.Codeberg {
		t.Fatalf("ID() = %v", got)
	}
}

func TestCodebergMapsReposIntoMatches(t *testing.T) {
	m := mustSearch(t, codebergFor(codebergServer(t, 200, codebergBody)), query())
	// The repository without a description is left out, as on GitHub.
	if len(m) != 1 || m[0].Name != "ctfrancia/gkp" || m[0].Source != model.Codeberg ||
		m[0].URL != "https://codeberg.org/ctfrancia/gkp" || m[0].Description != "GKP (Go Kill Port) kills processes on a port" ||
		deref(m[0].Popularity) != uint64(3) || deref(m[0].LastUpdated) != "2025-12-08T09:30:00Z" {
		t.Errorf("matches = %+v", m)
	}
}

func TestCodebergSendsSearchQueryParams(t *testing.T) {
	srv := codebergServer(t, 200, codebergBody)
	mustSearch(t, codebergFor(srv), query())
	p := srv.Requests()[0].URL.Query()
	if p.Get("q") != "async runtime" || p.Get("includeDesc") != "true" || p.Get("mode") != "source" ||
		p.Get("sort") != "stars" || p.Get("order") != "desc" || p.Get("limit") != "50" || len(srv.Requests()) != 1 {
		t.Errorf("params = %v", p)
	}
}

func TestCodebergEmptyResultsIsOK(t *testing.T) {
	if m := mustSearch(t, codebergFor(codebergServer(t, 200, `{"ok": true, "data": []}`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestCodebergServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, codebergFor(codebergServer(t, 500, `{"ok": false, "error": "search failed"}`)), query())
}

func TestCodebergChangedResponseShapeFailsTheSource(t *testing.T) {
	// A response without data, or a repository without a name or URL, must
	// fail the source rather than pass for no results.
	for _, body := range []string{
		`{"ok": true}`,
		strings.Replace(codebergBody, `"full_name": "ctfrancia/gkp", `, "", 1),
		strings.Replace(codebergBody, `"html_url": "https://codeberg.org/ctfrancia/gkp", `, "", 1),
	} {
		wantSearchError(t, codebergFor(codebergServer(t, 200, body)), query())
	}
}

func TestCodebergMalformedDateNeverFailsTheSource(t *testing.T) {
	body := strings.Replace(codebergBody, `"2025-12-08T10:30:00+01:00"`, `"yesterday"`, 1)
	if m := mustSearch(t, codebergFor(codebergServer(t, 200, body)), query()); len(m) != 1 || m[0].LastUpdated != nil {
		t.Errorf("matches = %+v", m)
	}
}

func TestCodebergTriesTheTwoLongestKeywordsThenTheLongest(t *testing.T) {
	// Each request takes seconds, so the steps of three keywords or more,
	// which never matched a phrase when measured, are skipped.
	srv := narrowingServer(t, "/api/v1/repos/search", "q", narrowingSteps[:3], `{"ok": true, "data": []}`, "mistakes", codebergBody)
	m := mustSearch(t, codebergFor(srv), narrowingQuery())
	if got := requested(srv, "q"); len(m) != 1 || m[0].Name != "ctfrancia/gkp" || !reflect.DeepEqual(got, []string{"syntax mistakes", "mistakes"}) {
		t.Errorf("matches = %+v after %q", m, got)
	}
	// A query of one keyword is sent once.
	srv = codebergServer(t, 200, `{"ok": true, "data": []}`)
	mustSearch(t, codebergFor(srv), model.Query{Idea: "anything", Keywords: []string{"forgejo"}})
	if got := requested(srv, "q"); !reflect.DeepEqual(got, []string{"forgejo"}) {
		t.Errorf("requests = %q", got)
	}
}

func TestCodebergKeepsNarrowingPastReposWithoutDescriptions(t *testing.T) {
	// A step whose only hits have no description counts as empty.
	undescribed := `{"ok": true, "data": [{"full_name": "a/b", "description": "", "html_url": "https://codeberg.org/a/b"}]}`
	srv := narrowingServer(t, "/api/v1/repos/search", "q", narrowingSteps[:3], undescribed, "mistakes", codebergBody)
	if m := mustSearch(t, codebergFor(srv), narrowingQuery()); len(m) != 1 || len(srv.Requests()) != 2 {
		t.Errorf("matches = %+v after %q", m, requested(srv, "q"))
	}
}

func TestCodebergWaitsLongerThanTheOtherSources(t *testing.T) {
	shared := NewHTTPClient()
	a := NewCodeberg(shared)
	if a.Client == shared || a.Client.Timeout != codebergRequestTimeout || a.Client.Transport != shared.Transport ||
		shared.Timeout != 10*time.Second {
		t.Errorf("client = %+v, shared = %+v", a.Client, shared)
	}
	// The source's time must hold a whole request at least.
	if a.searchTimeout() <= defaultFanout.timeout || a.searchTimeout() < codebergRequestTimeout {
		t.Errorf("search timeout = %v", a.searchTimeout())
	}
}
