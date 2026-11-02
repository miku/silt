package sources

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/miku/silt/internal/freshness"
	"github.com/miku/silt/internal/model"
)

const (
	hexBaseURL = "https://hex.pm"
	// hexTerms is how many of the longest keywords get a search of their own.
	hexTerms = 3
)

// Hex searches the Hex registry (Erlang, Elixir): GET /api/packages?search=.
type Hex struct {
	Client  *http.Client
	BaseURL string
}

// NewHex returns an adapter for the live Hex registry.
func NewHex(client *http.Client) *Hex { return &Hex{Client: client, BaseURL: hexBaseURL} }

// ID implements Adapter.
func (*Hex) ID() model.Source { return model.Hex }

// Search implements Adapter.
func (a *Hex) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// Hex's search grammar only honours a single bare term. Measured live: a
	// space-joined multi-word query returns the unfiltered catalogue (100
	// packages, alphabetical, identical for every idea), and with
	// description: prefixes only the last term filters. Narrowing cannot
	// detect that — the flood is never empty — so each of the longest
	// keywords is searched on its own and the results are merged.
	terms := slices.Clone(q.Keywords)
	slices.SortStableFunc(terms, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	terms = terms[:min(hexTerms, len(terms))]
	if len(terms) == 0 {
		terms = []string{q.Idea}
	}
	var matches []model.Match
	seen := make(map[string]bool)
	for _, term := range terms {
		var pkgs []struct {
			Name    *string `json:"name"`
			HTMLURL *string `json:"html_url"`
			Meta    *struct {
				Description string `json:"description"`
			} `json:"meta"`
			Downloads *struct {
				All *uint64 `json:"all"` // the lifetime count
			} `json:"downloads"`
			// The last release — RFC 3339 with microsecond precision.
			UpdatedAt freshness.Lenient[string] `json:"updated_at"`
		}
		params := queryParams{{"search", term}, {"page", "1"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/api/packages", params, &pkgs); err != nil {
			return nil, err
		}
		if pkgs == nil {
			return nil, missingField("packages") // a JSON null, not a list
		}
		for _, p := range pkgs {
			if p.Name == nil {
				return nil, missingField("name")
			}
			if seen[*p.Name] {
				continue
			}
			seen[*p.Name] = true
			m := model.Match{
				Name:        *p.Name,
				Source:      model.Hex,
				URL:         a.BaseURL + "/packages/" + *p.Name,
				LastUpdated: rfc3339Date(p.UpdatedAt),
			}
			// Prefer the registry's own web link.
			if p.HTMLURL != nil && strings.TrimSpace(*p.HTMLURL) != "" {
				m.URL = *p.HTMLURL
			}
			if p.Meta != nil {
				m.Description = p.Meta.Description
			}
			if p.Downloads != nil && p.Downloads.All != nil && *p.Downloads.All > 0 {
				m.Popularity = p.Downloads.All
			}
			matches = append(matches, m)
		}
	}
	return matches, nil
}
