package sources

import (
	"context"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/miku/silt"
	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

const goPkgDevBaseURL = "https://pkg.go.dev"

// GoPkgDev searches Go packages by scraping pkg.go.dev's search page,
// /search?q=, as there is no public JSON search API.
//
// Brittle by nature: if the markup changes, a non-trivial page that parses to
// zero packages is reported as drift (a ParseError), so that the source is
// surfaced as not reached rather than silently reading as "nothing out
// there". A page that genuinely has no matches is told apart from drift by
// its own marker and is an ordinary empty result.
type GoPkgDev struct {
	Client  *http.Client
	BaseURL string
}

// NewGoPkgDev returns an adapter for the live pkg.go.dev.
func NewGoPkgDev(client *http.Client) *GoPkgDev {
	return &GoPkgDev{Client: client, BaseURL: goPkgDevBaseURL}
}

// ID implements Adapter.
func (*GoPkgDev) ID() model.Source { return model.Go }

// Search implements Adapter.
func (a *GoPkgDev) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// pkg.go.dev ANDs every term, so a realistic multi-keyword idea reliably
	// returns nothing even where a matching package exists — measured live, a
	// 7-term idea returns zero results while 2-3 of its longest terms return
	// 100+.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 2) {
		resp, err := get(ctx, a.Client, a.BaseURL+"/search", queryParams{{"q", term}, {"m", "package"}})
		if err != nil {
			return nil, err
		}
		page, err := readText(resp)
		if err != nil {
			return nil, err
		}
		if matches, err = parseGoPkgDev(page, a.BaseURL); err != nil {
			return nil, err
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}

func parseGoPkgDev(page, baseURL string) ([]model.Match, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return nil, &silt.ParseError{Msg: err.Error()}
	}
	var matches []model.Match
	doc.Find(".SearchSnippet").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		link := s.Find("a[href]").First()
		if link.Length() == 0 {
			return true
		}
		// The title anchor holds the package name as its own text, then a
		// nested "(github.com/owner/repo)" span. Text() walks descendants, so
		// it would glue the module path onto the name — complete with the
		// markup's newlines and indentation. Read only the anchor's direct
		// text nodes, and collapse the remaining whitespace.
		var own strings.Builder
		for n := link.Nodes[0].FirstChild; n != nil; n = n.NextSibling {
			if n.Type == html.TextNode {
				own.WriteString(n.Data)
			}
		}
		name := strings.Join(strings.Fields(own.String()), " ")
		if name == "" {
			return true
		}
		desc := strings.TrimSpace(s.Find(".SearchSnippet-synopsis").First().Text())
		if desc == "" {
			desc = name
		}
		href, _ := link.Attr("href")
		m := model.Match{Name: name, Source: model.Go, URL: absURL(baseURL, href), Description: desc}
		// The only source whose date is scraped rather than read from an API
		// field, rendered as a human date ("Feb 28, 2026"). A missing or
		// reworded element just leaves the date empty; unlike the name and
		// URL, it never counts as drift worth failing over.
		if published := s.Find("[data-test-id='snippet-published']").First(); published.Length() > 0 {
			if d, ok := freshness.FromGoDate(published.Text()); ok {
				m.LastUpdated = &d
			}
		}
		matches = append(matches, m)
		return len(matches) < 20
	})

	if len(matches) == 0 {
		// A genuine "no matches" page is a real answer, not drift.
		// pkg.go.dev renders it with a gopher illustration carrying this test
		// id, present on every zero-result page and on no page that has
		// results (verified live). The empty page is ~33 KB, so without this
		// check the drift guard below would always fire.
		if doc.Find("[data-test-id='gopher-message']").Length() > 0 {
			return matches, nil
		}
		if len(page) > 2000 {
			return nil, &silt.ParseError{Msg: "pkg.go.dev search page structure may have changed -- zero packages parsed from a non-trivial response"}
		}
	}
	return matches, nil
}
