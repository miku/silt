package sources

import (
	"context"
	"net/http"
	"net/url"

	"github.com/miku/silt/model"
)

const rubyGemsBaseURL = "https://rubygems.org"

// RubyGems searches RubyGems: GET /api/v1/search.json?query=.
type RubyGems struct {
	Client  *http.Client
	BaseURL string
}

// NewRubyGems returns an adapter for the live RubyGems registry.
func NewRubyGems(client *http.Client) *RubyGems {
	return &RubyGems{Client: client, BaseURL: rubyGemsBaseURL}
}

// ID implements Adapter.
func (*RubyGems) ID() model.Source { return model.RubyGems }

// Search implements Adapter.
func (a *RubyGems) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// RubyGems ANDs every term, so a realistic multi-keyword idea returns
	// nothing even where a matching gem exists — measured live, a 7-term idea
	// returns zero while its 2 longest terms return plenty.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 2) {
		var gems []struct {
			Name       *string `json:"name"`
			Info       string  `json:"info"`
			ProjectURI *string `json:"project_uri"`
			Downloads  *uint64 `json:"downloads"`
		}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/api/v1/search.json", url.Values{"query": {term}}, &gems); err != nil {
			return nil, err
		}
		if gems == nil {
			return nil, missingField("gems") // a JSON null, not a list
		}
		matches = matches[:0]
		for _, g := range gems {
			if g.Name == nil {
				return nil, missingField("name")
			}
			if g.Info == "" || len(matches) == 20 {
				continue
			}
			m := model.Match{
				Name:        *g.Name,
				Source:      model.RubyGems,
				URL:         "https://rubygems.org/gems/" + *g.Name,
				Description: g.Info,
				Popularity:  g.Downloads,
			}
			if g.ProjectURI != nil {
				m.URL = *g.ProjectURI
			}
			matches = append(matches, m)
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}
