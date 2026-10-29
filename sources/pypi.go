package sources

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

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
// counts then. With Index, a walled search that no browser got through
// matches the keywords against the names of all projects instead (see
// searchIndex). Without either, or if both fail, PyPI is unavailable.
type PyPI struct {
	Client  *http.Client
	BaseURL string
	// Browser, if not nil, fetches the search page when the plain request
	// is walled.
	Browser Browser
	// Index, if set, matches the keywords against PyPI's index of project
	// names when the plain request is walled and no Browser got through.
	Index bool
	// Catalogs, if not nil, keeps the index on disk, so that the runs within
	// its TTL don't download it again.
	Catalogs *CatalogCache

	// index is the shared, process-wide index for adapters from NewPyPI;
	// an adapter built otherwise keeps its own.
	index *memCatalog[[]string]
	init  sync.Once
}

// NewPyPI returns an adapter for the live PyPI site, which shares its index
// with every other adapter NewPyPI returns.
func NewPyPI(client *http.Client) *PyPI {
	return &PyPI{Client: client, BaseURL: pypiBaseURL, index: sharedPyPIIndex}
}

// ID implements Adapter.
func (*PyPI) ID() model.Source { return model.PyPI }

// Search implements Adapter.
func (a *PyPI) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	matches, _, err := a.searchDetour(ctx, q)
	return matches, err
}

// searchDetour implements detouringAdapter.
func (a *PyPI) searchDetour(ctx context.Context, q model.Query) ([]model.Match, detour, error) {
	params := queryParams{{"q", strings.Join(q.Keywords, " ")}}
	matches, err := a.searchPlain(ctx, params)
	var walled *silt.UnavailableError
	if !errors.As(err, &walled) {
		return matches, noDetour, err
	}
	why := []string{pypiUnavailable}
	if a.Browser != nil {
		matches, err := a.searchBrowser(ctx, params)
		if err == nil {
			return matches, throughBrowser, nil
		}
		why = append(why, err.Error())
	}
	if a.Index {
		matches, err := a.searchIndex(ctx, q)
		if err == nil {
			return matches, byName, nil
		}
		why = append(why, "its index of names: "+err.Error())
	}
	return nil, noDetour, &silt.UnavailableError{Msg: strings.Join(why, "; ")}
}

// searchBrowser searches with the browser. Only a page that is recognisably
// PyPI's results counts.
func (a *PyPI) searchBrowser(ctx context.Context, params queryParams) ([]model.Match, error) {
	html, err := a.Browser.DumpDOM(ctx, a.BaseURL+"/search/?"+params.encode())
	if err != nil {
		return nil, err
	}
	matches, noResults, err := scrapePyPI(html, a.BaseURL)
	if err != nil || len(matches) == 0 && !noResults {
		return nil, errors.New("the browser did not get a results page either")
	}
	return matches, nil
}

// searchPlain searches with an HTTP request.
func (a *PyPI) searchPlain(ctx context.Context, params queryParams) ([]model.Match, error) {
	resp, err := get(ctx, a.Client, a.BaseURL+"/search/", params)
	if err != nil {
		return nil, err
	}
	// A hard 403 is the bot wall's blunt form; a 200 with a challenge stub is
	// caught in parsePyPI. Either way it's a persistent wall, not a transient
	// error. A 429 or 5xx is still a retryable HTTPError (a 429 also a
	// rateLimitError).
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
