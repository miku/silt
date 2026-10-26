package sources

import (
	"context"
	"errors"
	"strings"
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
