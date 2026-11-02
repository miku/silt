package sources

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/miku/silt/internal/model"
)

const goHTML = `<!doctype html><html><body>
  <div class="SearchSnippet">
    <a href="/github.com/spf13/cobra">cobra</a>
    <span data-test-id="snippet-published"><strong>Feb 28, 2026</strong></span>
    <p class="SearchSnippet-synopsis">A Commander for modern Go CLI interactions.</p>
  </div>
</body></html>`

// goNoMatchesHTML is a real pkg.go.dev zero-result page: large, and carrying
// the "no matches" gopher. Padded past the 2 KB drift threshold on purpose:
// the live page is ~33 KB, so the threshold alone can never tell it apart
// from drift.
var goNoMatchesHTML = `<!doctype html><html><body>
  <div data-test-id="gopher-message">It looks like there are no matches for your search.</div>
  <!-- ` + strings.Repeat("x", 2500) + ` -->
</body></html>`

// goNestedTitleHTML is the real title anchor: the package name as the
// anchor's own text, then a nested span holding the module path, separated
// by markup newlines.
const goNestedTitleHTML = `<!doctype html><html><body>
  <div class="SearchSnippet">
    <h2>
      <a href="/github.com/julienschmidt/httprouter" data-test-id="snippet-title">
        httprouter
        <span class="SearchSnippet-header-path">(github.com/julienschmidt/httprouter)</span>
      </a>
    </h2>
    <p class="SearchSnippet-synopsis">A trie based high performance HTTP request router.</p>
  </div>
</body></html>`

func goFor(srv *mockServer) *GoPkgDev { return &GoPkgDev{Client: NewHTTPClient(), BaseURL: srv.URL} }

func goServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/search", respond(status, body)))
}

func TestGoIDIsGo(t *testing.T) {
	if got := NewGoPkgDev(nil).ID(); got != model.Go {
		t.Fatalf("ID() = %v", got)
	}
}

func TestGoScrapesPackagesIntoMatches(t *testing.T) {
	srv := goServer(t, 200, goHTML)
	m, err := goFor(srv).Search(context.Background(), query())
	if err != nil || len(m) != 1 {
		t.Fatalf("Search = %+v, %v", m, err)
	}
	if m[0].Name != "cobra" || m[0].Source != model.Go || !strings.HasSuffix(m[0].URL, "/github.com/spf13/cobra") ||
		m[0].Description != "A Commander for modern Go CLI interactions." {
		t.Errorf("match = %+v", m[0])
	}
	params := srv.Requests()[0].URL.Query()
	if params.Get("q") != "async runtime" || params.Get("m") != "package" {
		t.Errorf("params = %v", params)
	}
}

func TestGoServerErrorIsPropagated(t *testing.T) {
	if _, err := goFor(goServer(t, 500, "")).Search(context.Background(), query()); err == nil {
		t.Error("a 5xx response must surface as an error")
	}
}

func TestGoEmptyResultsIsOK(t *testing.T) {
	m, err := goFor(goServer(t, 200, "<!doctype html><html><body><p>No results.</p></body></html>")).Search(context.Background(), query())
	if err != nil || len(m) != 0 {
		t.Errorf("Search = %+v, %v", m, err)
	}
}

func TestGoNameExcludesTheNestedModulePath(t *testing.T) {
	m, err := goFor(goServer(t, 200, goNestedTitleHTML)).Search(context.Background(), query())
	if err != nil || len(m) != 1 {
		t.Fatalf("Search = %+v, %v", m, err)
	}
	// Not "httprouter (github.com/julienschmidt/httprouter)", and no
	// embedded newlines or run-together indentation from the markup.
	if m[0].Name != "httprouter" {
		t.Errorf("name = %q", m[0].Name)
	}
}

func TestGoGenuineNoMatchesIsNotReportedAsDrift(t *testing.T) {
	// A source that answered "nothing found" is reached; reporting it as not
	// reached overstates how little we know.
	m, err := goFor(goServer(t, 200, goNoMatchesHTML)).Search(context.Background(), query())
	if err != nil || len(m) != 0 {
		t.Errorf("Search = %+v, %v", m, err)
	}
}

func TestGoNonTrivialPageWithoutTheNoMatchesMarkerIsDrift(t *testing.T) {
	page := "<!doctype html><html><body><div>" + strings.Repeat("x", 2500) + "</div></body></html>"
	if _, err := goFor(goServer(t, 200, page)).Search(context.Background(), query()); err == nil {
		t.Error("want a drift error")
	}
}

// goNarrowing serves the no-matches page for each of empty and goHTML for
// found.
func goNarrowing(t *testing.T, empty []string, found string) *mockServer {
	routes := map[string]http.Handler{found: respond(200, goHTML)}
	for _, q := range empty {
		routes[q] = respond(200, goNoMatchesHTML)
	}
	return newMockServer(t, onPath("/search", byParam("q", routes)))
}

func TestGoNarrowsToThreeLongestKeywordsWhenFullQueryIsEmpty(t *testing.T) {
	srv := goNarrowing(t, []string{"ide code spell syntax mistakes"}, "spell syntax mistakes")
	m, err := goFor(srv).Search(context.Background(), narrowingQuery())
	if err != nil || len(m) != 1 || m[0].Name != "cobra" || len(srv.Requests()) != 2 {
		t.Errorf("Search = %+v, %v after %d requests", m, err, len(srv.Requests()))
	}
}

func TestGoFallsBackToTwoLongestKeywords(t *testing.T) {
	srv := goNarrowing(t, []string{"ide code spell syntax mistakes", "spell syntax mistakes"}, "syntax mistakes")
	m, err := goFor(srv).Search(context.Background(), narrowingQuery())
	if err != nil || len(m) != 1 || len(srv.Requests()) != 3 {
		t.Errorf("Search = %+v, %v after %d requests", m, err, len(srv.Requests()))
	}
}

func TestGoAllNarrowingAttemptsEmptyIsOKNotErr(t *testing.T) {
	srv := goNarrowing(t, []string{"ide code spell syntax mistakes", "spell syntax mistakes", "syntax mistakes"}, "unused")
	m, err := goFor(srv).Search(context.Background(), narrowingQuery())
	if err != nil || len(m) != 0 || len(srv.Requests()) != 3 {
		t.Errorf("Search = %+v, %v after %d requests", m, err, len(srv.Requests()))
	}
}

func TestGoParsesTheRenderedPublicationDate(t *testing.T) {
	m, err := goFor(goServer(t, 200, goHTML)).Search(context.Background(), query())
	if err != nil || deref(m[0].LastUpdated) != "2026-02-28T00:00:00Z" {
		t.Errorf("Search = %+v, %v", m, err)
	}
}

// pkg.go.dev is scraped, so its date drifts differently: the element can
// move, vanish, or start rendering a format we don't know.
func TestGoSurvivesAnUnparseableRenderedDate(t *testing.T) {
	for _, page := range []string{
		strings.Replace(goHTML, "Feb 28, 2026", "Smarch 40, 2026", 1),
		strings.Replace(goHTML, "Feb 28, 2026", "", 1),
		strings.Replace(goHTML, "snippet-published", "snippet-renamed", 1),
	} {
		m, err := goFor(goServer(t, 200, page)).Search(context.Background(), query())
		if err != nil || len(m) != 1 || m[0].LastUpdated != nil {
			t.Errorf("Search = %+v, %v", m, err)
		}
	}
}

func TestGoKeepsAtMost20AndFallsBackToTheNameForTheSynopsis(t *testing.T) {
	var b strings.Builder
	for i := range 25 {
		fmt.Fprintf(&b, `<div class="SearchSnippet"><a href="https://pkg.go.dev/x/p%d">p%d</a></div>`, i, i)
	}
	m, err := goFor(goServer(t, 200, b.String())).Search(context.Background(), query())
	if err != nil || len(m) != 20 {
		t.Fatalf("Search = %d matches, %v", len(m), err)
	}
	if m[0].Description != "p0" || m[0].URL != "https://pkg.go.dev/x/p0" {
		t.Errorf("first = %+v", m[0])
	}
}
