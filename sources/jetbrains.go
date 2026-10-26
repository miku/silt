package sources

import (
	"context"
	"net/http"
	"strings"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

const (
	jetBrainsBaseURL    = "https://plugins.jetbrains.com"
	maxJetBrainsDescLen = 120
)

// JetBrains searches the JetBrains Marketplace: GET /api/searchPlugins.
type JetBrains struct {
	Client *http.Client
	// BaseURL is the marketplace host. Plugin links are relative to it.
	BaseURL string
}

// NewJetBrains returns an adapter for the live JetBrains Marketplace.
func NewJetBrains(client *http.Client) *JetBrains {
	return &JetBrains{Client: client, BaseURL: jetBrainsBaseURL}
}

// ID implements Adapter.
func (*JetBrains) ID() model.Source { return model.JetBrains }

type jetBrainsPlugin struct {
	Name      *string `json:"name"`
	Link      *string `json:"link"`
	Preview   *string `json:"preview"`
	Downloads *uint64 `json:"downloads"`
	// Last update, in epoch milliseconds (not the creation date).
	Cdate freshness.Lenient[int64] `json:"cdate"`
}

// Search implements Adapter.
func (a *JetBrains) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// searchPlugins ANDs every content word in the query, so a realistic
	// multi-keyword idea (5+ terms) reliably returns zero hits even when a
	// matching plugin exists — measured live: a 5-7 term idea returns
	// total: 0, while the 3 longest terms of the same idea routinely return
	// results.
	var plugins []jetBrainsPlugin
	for _, term := range narrowingCandidates(q, 2) {
		var body struct {
			Plugins *[]jetBrainsPlugin `json:"plugins"`
		}
		params := queryParams{{"search", term}, {"max", "20"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/api/searchPlugins", params, &body); err != nil {
			return nil, err
		}
		if body.Plugins == nil {
			return nil, missingField("plugins")
		}
		if len(*body.Plugins) > 0 {
			plugins = *body.Plugins
			break
		}
	}
	var matches []model.Match
	for _, p := range plugins {
		if p.Name == nil || strings.TrimSpace(*p.Name) == "" ||
			p.Link == nil || !strings.HasPrefix(strings.TrimSpace(*p.Link), "/") {
			continue
		}
		desc := *p.Name
		if p.Preview != nil {
			if s := strings.TrimSpace(*p.Preview); s != "" {
				desc = s
			}
		}
		m := model.Match{
			Name:        *p.Name,
			Source:      model.JetBrains,
			URL:         a.BaseURL + strings.TrimSpace(*p.Link),
			Description: truncate(desc, maxJetBrainsDescLen),
			Popularity:  p.Downloads,
		}
		if p.Cdate.OK {
			if s, ok := freshness.FromUnixMillis(p.Cdate.V); ok {
				m.LastUpdated = &s
			}
		}
		matches = append(matches, m)
	}
	return matches, nil
}
