package sources

import (
	"context"
	"net/http"
	"net/url"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

const cratesIOBaseURL = "https://crates.io"

// CratesIO searches the crates.io registry: GET /api/v1/crates?q=.
type CratesIO struct {
	Client *http.Client
	// BaseURL is the registry host, without a trailing slash. Result links
	// are built from it too, so a mirror or mock server links to itself.
	BaseURL string
}

// NewCratesIO returns an adapter for the live crates.io host.
func NewCratesIO(client *http.Client) *CratesIO {
	return &CratesIO{Client: client, BaseURL: cratesIOBaseURL}
}

// ID implements Adapter.
func (*CratesIO) ID() model.Source { return model.CratesIO }

// Search implements Adapter.
func (a *CratesIO) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// crates.io ANDs every term across name and description — measured live,
	// a 7-term idea returns 3 unrelated crates and a 5-term one 121, while 2
	// of its longest terms return tens of thousands.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 2) {
		var body struct {
			Crates *[]struct {
				Name        *string                   `json:"name"`
				Description string                    `json:"description"`
				Downloads   *uint64                   `json:"downloads"`
				UpdatedAt   freshness.Lenient[string] `json:"updated_at"` // last publish of any version
			} `json:"crates"`
		}
		params := url.Values{"q": {term}, "per_page": {"20"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/api/v1/crates", params, &body); err != nil {
			return nil, err
		}
		if body.Crates == nil {
			return nil, missingField("crates")
		}
		matches = matches[:0]
		for _, c := range *body.Crates {
			if c.Name == nil {
				return nil, missingField("name")
			}
			matches = append(matches, model.Match{
				Name:        *c.Name,
				Source:      model.CratesIO,
				URL:         a.BaseURL + "/crates/" + *c.Name,
				Description: c.Description,
				Popularity:  c.Downloads,
				LastUpdated: rfc3339Date(c.UpdatedAt),
			})
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}
