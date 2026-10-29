package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

const pypiHTML = `<!doctype html><html><body>
  <ul>
    <li>
      <a class="package-snippet" href="/project/requests/">
        <h3 class="package-snippet__title">
          <span class="package-snippet__name">requests</span>
          <span class="package-snippet__version">2.31.0</span>
        </h3>
        <p class="package-snippet__description">Python HTTP for Humans.</p>
      </a>
    </li>
    <li>
      <a class="package-snippet" href="/project/httpx/">
        <h3 class="package-snippet__title">
          <span class="package-snippet__name">httpx</span>
          <span class="package-snippet__version">0.27.0</span>
        </h3>
        <p class="package-snippet__description">A next-gen HTTP client.</p>
      </a>
    </li>
  </ul>
</body></html>`

func pypiFor(srv *mockServer) *PyPI { return &PyPI{Client: NewHTTPClient(), BaseURL: srv.URL} }

func pypiServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/search/", respond(status, body)))
}

func TestPyPIIDIsPyPI(t *testing.T) {
	if got := NewPyPI(nil).ID(); got != model.PyPI {
		t.Fatalf("ID() = %v", got)
	}
}

func TestPyPIScrapesPackagesIntoMatches(t *testing.T) {
	m, err := pypiFor(pypiServer(t, 200, pypiHTML)).Search(context.Background(), query())
	if err != nil || len(m) != 2 {
		t.Fatalf("Search = %+v, %v", m, err)
	}
	first := m[0]
	// The URL is built from the (mock) base URL and the relative href.
	if first.Name != "requests" || first.Source != model.PyPI || !strings.HasSuffix(first.URL, "/project/requests/") ||
		first.Description != "Python HTTP for Humans." || first.Popularity != nil || first.Similarity != 0 {
		t.Errorf("first = %+v", first)
	}
	if m[1].Name != "httpx" || !strings.HasSuffix(m[1].URL, "/project/httpx/") {
		t.Errorf("second = %+v", m[1])
	}
}

func TestPyPISendsQQueryParam(t *testing.T) {
	srv := pypiServer(t, 200, pypiHTML)
	if _, err := pypiFor(srv).Search(context.Background(), query()); err != nil {
		t.Fatal(err)
	}
	if got := srv.Requests()[0].URL.Query().Get("q"); got != "async runtime" {
		t.Errorf("q = %q", got)
	}
}

func TestPyPINoSnippetsIsOKEmpty(t *testing.T) {
	m, err := pypiFor(pypiServer(t, 200, "<!doctype html><html><body><p>No results.</p></body></html>")).Search(context.Background(), query())
	if err != nil || len(m) != 0 {
		t.Errorf("Search = %+v, %v", m, err)
	}
}

func TestPyPIServerErrorIsPropagated(t *testing.T) {
	_, err := pypiFor(pypiServer(t, 500, "")).Search(context.Background(), query())
	if err == nil || !silt.IsRetryable(err) {
		t.Errorf("err = %v; a 5xx must be a retryable error", err)
	}
}

func TestPyPIForbiddenIsUnavailable(t *testing.T) {
	// A hard 403 from the bot wall must surface as Unavailable
	// (non-retryable), not a generic HTTP error, so that the reason is
	// accurate and the retry is skipped.
	_, err := pypiFor(pypiServer(t, 403, "")).Search(context.Background(), query())
	var unavailable *silt.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("403 bot wall should be an UnavailableError, got: %v", err)
	}
}

func TestPyPIBotWall200ChallengeIsUnavailable(t *testing.T) {
	// The live wall serves a 200 whose body is a JS challenge stub: a
	// non-trivial page (>2000 bytes) with zero package snippets.
	stub := "<!doctype html><html><head><title>Client Challenge</title></head><body>" + strings.Repeat("x", 2500) + "</body></html>"
	_, err := pypiFor(pypiServer(t, 200, stub)).Search(context.Background(), query())
	var unavailable *silt.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("200 challenge stub should be an UnavailableError, got: %v", err)
	}
}

func TestPyPISkipsSnippetsWithoutAName(t *testing.T) {
	page := `<a class="package-snippet" href="https://elsewhere.example/x/"><span class="package-snippet__name"> </span></a>` +
		`<a class="package-snippet" href="https://elsewhere.example/y/"><span class="package-snippet__name">y</span></a>`
	m, err := pypiFor(pypiServer(t, 200, page)).Search(context.Background(), query())
	if err != nil || len(m) != 1 || m[0].URL != "https://elsewhere.example/y/" || m[0].Description != "" {
		t.Errorf("Search = %+v, %v; absolute hrefs are kept as they are", m, err)
	}
}

// fakeBrowser stands in for internal/browser: it returns dom, or err, and
// records the URLs it was asked for.
type fakeBrowser struct {
	dom  string
	err  error
	urls []string
}

func (b *fakeBrowser) DumpDOM(_ context.Context, url string) (string, error) {
	b.urls = append(b.urls, url)
	return b.dom, b.err
}

// pypiChallenge is the wall: a 200 with a JS challenge and no results.
var pypiChallenge = "<!doctype html><html><head><title>Client Challenge</title></head><body>" + strings.Repeat("x", 2500) + "</body></html>"

// pypiNoResults is the page PyPI renders for a search without results: as
// large as one with results, and saying there were none.
var pypiNoResults = `<!doctype html><html><head><title>Search results · PyPI</title></head><body>` + strings.Repeat("<nav>menu</nav>", 300) +
	`<form><div><div class="callout-block"><p>
There were no results for 'zqxjvkwplmnb'          </p></div></div></form></body></html>`

func TestPyPIGoesThroughTheBrowserWhenWalled(t *testing.T) {
	for _, wall := range []struct {
		status int
		body   string
	}{{403, ""}, {200, pypiChallenge}} {
		srv := pypiServer(t, wall.status, wall.body)
		b := &fakeBrowser{dom: pypiHTML}
		a := pypiFor(srv)
		a.Browser = b
		m, browsed, err := a.searchReportingBrowser(t.Context(), query())
		if err != nil || !browsed || len(m) != 2 || m[0].Name != "requests" || !strings.HasPrefix(m[0].URL, srv.URL) {
			t.Errorf("wall %d: Search = %+v, browsed %v, %v", wall.status, m, browsed, err)
		}
		if want := []string{srv.URL + "/search/?q=async+runtime"}; !slices.Equal(b.urls, want) {
			t.Errorf("wall %d: the browser loaded %q, want %q", wall.status, b.urls, want)
		}
	}
}

func TestPyPIOnlyUsesTheBrowserForTheWall(t *testing.T) {
	for _, tt := range []struct {
		status int
		body   string
	}{{200, pypiHTML}, {200, pypiNoResults}, {500, ""}, {429, ""}} {
		b := &fakeBrowser{dom: pypiHTML}
		a := pypiFor(pypiServer(t, tt.status, tt.body))
		a.Browser = b
		_, browsed, err := a.searchReportingBrowser(t.Context(), query())
		if browsed || len(b.urls) != 0 {
			t.Errorf("%d: the browser was used for a page that isn't the wall", tt.status)
		}
		if tt.status >= 400 && !silt.IsRetryable(err) {
			t.Errorf("%d: err = %v, want the retryable error as before", tt.status, err)
		}
	}
}

func TestPyPIStaysUnavailableWhenTheBrowserFails(t *testing.T) {
	for name, b := range map[string]*fakeBrowser{
		"browser error":    {err: errors.New("the browser: exit status 3: Missing X server")},
		"walled again":     {dom: pypiChallenge},
		"blank page":       {dom: "<html><head></head><body></body></html>"},
		"another site":     {dom: "<html><body><p>Captive portal: please log in</p></body></html>"},
		"truncated output": {dom: ""},
	} {
		a := pypiFor(pypiServer(t, 200, pypiChallenge))
		a.Browser = b
		m, browsed, err := a.searchReportingBrowser(t.Context(), query())
		var unavailable *silt.UnavailableError
		if !errors.As(err, &unavailable) || browsed || m != nil {
			t.Errorf("%s: Search = %+v, browsed %v, %v; want PyPI unavailable", name, m, browsed, err)
		}
	}
}

func TestPyPINoResultsIsAnEmptyResultWhateverTheSize(t *testing.T) {
	m, err := pypiFor(pypiServer(t, 200, pypiNoResults)).Search(t.Context(), query())
	if err != nil || len(m) != 0 {
		t.Errorf("plain: Search = %+v, %v; PyPI said there were no results", m, err)
	}
	a := pypiFor(pypiServer(t, 200, pypiChallenge))
	a.Browser = &fakeBrowser{dom: pypiNoResults}
	m, browsed, err := a.searchReportingBrowser(t.Context(), query())
	if err != nil || !browsed || len(m) != 0 {
		t.Errorf("browser: Search = %+v, browsed %v, %v", m, browsed, err)
	}
}

func TestSearcherReportsAndCachesASearchThroughTheBrowser(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html"}},
			Body:       io.NopCloser(strings.NewReader(pypiChallenge)),
			Request:    req,
		}, nil
	})}
	b := &fakeBrowser{dom: pypiHTML}
	s := Searcher{Client: client, Cache: testCache(t), Browser: b}
	include := []model.Source{model.PyPI}

	first := s.SearchFiltered(t.Context(), query(), include, nil)
	if !slices.Equal(first.Reached, include) || !slices.Equal(first.Browsed, include) || len(first.Cached) != 0 || len(first.Matches) != 2 {
		t.Fatalf("first: reached %v, browsed %v, cached %v, %d matches", first.Reached, first.Browsed, first.Cached, len(first.Matches))
	}
	second := s.SearchFiltered(t.Context(), query(), include, nil)
	if !slices.Equal(second.Cached, include) || len(second.Browsed) != 0 || len(b.urls) != 1 || requests.Load() != 1 {
		t.Errorf("second: cached %v, browsed %v, %d browser loads, %d requests; want it answered from the cache",
			second.Cached, second.Browsed, len(b.urls), requests.Load())
	}

	// Without a browser, the wall leaves PyPI not reached, as before.
	out := Searcher{Client: client}.SearchFiltered(t.Context(), query(), include, nil)
	if !slices.Equal(out.Failed, include) || len(out.Browsed) != 0 {
		t.Errorf("no browser: failed %v, browsed %v", out.Failed, out.Browsed)
	}
}
