package sources

import (
	"context"
	"net/http"

	"github.com/miku/silt/model"
)

const packagistBaseURL = "https://packagist.org"

// Packagist searches the Packagist (PHP/Composer) registry: GET
// /search.json?q=.
type Packagist struct {
	Client  *http.Client
	BaseURL string
}

// NewPackagist returns an adapter for the live Packagist host.
func NewPackagist(client *http.Client) *Packagist {
	return &Packagist{Client: client, BaseURL: packagistBaseURL}
}

// ID implements Adapter.
func (*Packagist) ID() model.Source { return model.Packagist }

// Search implements Adapter.
func (a *Packagist) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// Packagist ANDs every term, so a realistic multi-keyword idea returns
	// nothing even where a matching package exists — measured live, a 7-term
	// idea returns zero while its 2 longest terms return plenty.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 2) {
		var body struct {
			Results *[]struct {
				Name        *string `json:"name"`
				Description string  `json:"description"`
				// The package page, which Packagist returns as an absolute URL.
				URL       *string `json:"url"`
				Downloads *uint64 `json:"downloads"` // total installs
				Favers    *uint64 `json:"favers"`    // stars
			} `json:"results"`
		}
		params := queryParams{{"q", term}, {"per_page", "15"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/search.json", params, &body); err != nil {
			return nil, err
		}
		if body.Results == nil {
			return nil, missingField("results")
		}
		matches = matches[:0]
		for _, p := range *body.Results {
			if p.Name == nil {
				return nil, missingField("name")
			}
			m := model.Match{
				Name:        *p.Name,
				Source:      model.Packagist,
				URL:         a.BaseURL + "/packages/" + *p.Name,
				Description: p.Description,
			}
			if p.URL != nil {
				m.URL = *p.URL
			}
			// Popularity is the downloads, falling back to the stars; never 0.
			switch {
			case p.Downloads != nil && *p.Downloads > 0:
				m.Popularity = p.Downloads
			case p.Favers != nil && *p.Favers > 0:
				m.Popularity = p.Favers
			}
			matches = append(matches, m)
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}
