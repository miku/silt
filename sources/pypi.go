package sources

import (
	"context"
	"errors"
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
// wall for "nothing out there". A genuinely empty results page, one that is
// small or says there were no results, is still a legitimate empty result.
//
// With a Browser, a walled search is sent once more, through the browser,
// which passes the challenge. Only a page that is recognisably PyPI's results
// counts then; anything else leaves PyPI unavailable.
type PyPI struct {
	Client  *http.Client
	BaseURL string
	// Browser, if not nil, fetches the search page when the plain request
	// is walled.
	Browser Browser
}

// NewPyPI returns an adapter for the live PyPI site.
func NewPyPI(client *http.Client) *PyPI { return &PyPI{Client: client, BaseURL: pypiBaseURL} }

// ID implements Adapter.
func (*PyPI) ID() model.Source { return model.PyPI }

// Search implements Adapter.
func (a *PyPI) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	matches, _, err := a.searchReportingBrowser(ctx, q)
	return matches, err
}

// searchReportingBrowser implements browsingAdapter.
func (a *PyPI) searchReportingBrowser(ctx context.Context, q model.Query) ([]model.Match, bool, error) {
	params := queryParams{{"q", strings.Join(q.Keywords, " ")}}
	matches, err := a.searchPlain(ctx, params)
	var walled *silt.UnavailableError
	if a.Browser == nil || !errors.As(err, &walled) {
		return matches, false, err
	}
	html, err := a.Browser.DumpDOM(ctx, a.BaseURL+"/search/?"+params.encode())
	if err != nil {
		return nil, false, &silt.UnavailableError{Msg: pypiUnavailable + "; " + err.Error()}
	}
	matches, noResults, err := scrapePyPI(html, a.BaseURL)
	if err != nil || len(matches) == 0 && !noResults {
		return nil, false, &silt.UnavailableError{Msg: pypiUnavailable + "; the browser did not get a results page either"}
	}
	return matches, true, nil
}

// searchPlain searches with an HTTP request.
func (a *PyPI) searchPlain(ctx context.Context, params queryParams) ([]model.Match, error) {
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
	matches, noResults, err := scrapePyPI(html, baseURL)
	if err != nil {
		return nil, err
	}
	// A non-trivial page with no packages, which doesn't say there were
	// none: either PyPI's markup drifted or (the current reality) the body
	// is a bot-challenge stub. Both mean we could not search.
	if len(matches) == 0 && !noResults && len(html) > 2000 {
		return nil, &silt.UnavailableError{Msg: pypiUnavailable}
	}
	return matches, nil
}

// scrapePyPI returns the packages on a PyPI search results page, and whether
// the page says there were no results.
func scrapePyPI(html, baseURL string) (matches []model.Match, noResults bool, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, false, &silt.ParseError{Msg: err.Error()}
	}
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
	// The page PyPI renders for a search without results is as large as one
	// with results, and says so in a callout.
	noResults = strings.Contains(doc.Find(".callout-block").Text(), "There were no results for")
	return matches, noResults, nil
}

// absURL resolves a scraped href the way patent does: kept if it starts with
// "http", else appended to the base URL.
func absURL(baseURL, href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	return baseURL + href
}
