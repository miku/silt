package sources

import (
	"context"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

const pypiBaseURL = "https://pypi.org"

// pypiUnavailable is the accurate, user-facing reason given when PyPI's
// search is walled. One constant, so that the 403 and the JS-challenge paths
// report identically.
const pypiUnavailable = "PyPI search is unavailable to non-browser clients (no keyless search API; the search page is bot-walled)"

// PyPI searches PyPI by scraping its search page, /search/?q=.
//
// PyPI has no public search API (the XML-RPC endpoint was disabled), and the
// search page is now served behind a bot challenge to non-browser clients, so
// in practice the scrape is walled: either a hard 403, or a 200 whose body is
// a JS challenge stub that matches no snippet. Both surface as an
// UnavailableError with accurate wording — never a silent empty result, and
// never a misleading "markup may have changed" — so the fan-out reports PyPI
// as not reached (and skips the retry it can't win) rather than mistaking the
// wall for "nothing out there". A genuinely empty (small) results page is
// still a legitimate empty result.
type PyPI struct {
	Client  *http.Client
	BaseURL string
}

// NewPyPI returns an adapter for the live PyPI site.
func NewPyPI(client *http.Client) *PyPI { return &PyPI{Client: client, BaseURL: pypiBaseURL} }

// ID implements Adapter.
func (*PyPI) ID() model.Source { return model.PyPI }

// Search implements Adapter.
func (a *PyPI) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	params := queryParams{{"q", strings.Join(q.Keywords, " ")}}
	resp, err := get(ctx, a.Client, a.BaseURL+"/search/", params)
	if err != nil {
		return nil, err
	}
	// A hard 403 is the bot wall's blunt form; a 200 with a challenge stub is
	// caught in parsePyPI. Either way it's a persistent wall, not a transient
	// error. A 429 or 5xx is still a retryable HTTPError.
	if resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, &silt.UnavailableError{Msg: pypiUnavailable}
	}
	html, err := readText(resp)
	if err != nil {
		return nil, err
	}
	return parsePyPI(html, a.BaseURL)
}

// parsePyPI parses a PyPI search results page. A package with no name is
// skipped; a missing description becomes empty.
func parsePyPI(html, baseURL string) ([]model.Match, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, &silt.ParseError{Msg: err.Error()}
	}
	var matches []model.Match
	doc.Find("a.package-snippet").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find(".package-snippet__name").First().Text())
		if name == "" {
			return
		}
		href, _ := s.Attr("href")
		matches = append(matches, model.Match{
			Name:        name,
			Source:      model.PyPI,
			URL:         absURL(baseURL, href),
			Description: strings.TrimSpace(s.Find(".package-snippet__description").First().Text()),
		})
	})
	// A non-trivial page with no packages: either PyPI's markup drifted or
	// (the current reality) the body is a bot-challenge stub. Both mean we
	// could not search.
	if len(matches) == 0 && len(html) > 2000 {
		return nil, &silt.UnavailableError{Msg: pypiUnavailable}
	}
	return matches, nil
}

// absURL resolves a scraped href the way patent does: kept if it starts with
// "http", else appended to the base URL.
func absURL(baseURL, href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	return baseURL + href
}
