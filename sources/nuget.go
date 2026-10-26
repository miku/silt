package sources

import (
	"context"
	"net/http"
	"net/url"

	"github.com/miku/silt/model"
)

const nuGetBaseURL = "https://azuresearch-usnc.nuget.org"

// NuGet searches the NuGet gallery's search service: GET /query?q=.
type NuGet struct {
	Client *http.Client
	// BaseURL is the search service's host; result links point to
	// nuget.org.
	BaseURL string
}

// NewNuGet returns an adapter for the live NuGet search service.
func NewNuGet(client *http.Client) *NuGet { return &NuGet{Client: client, BaseURL: nuGetBaseURL} }

// ID implements Adapter.
func (*NuGet) ID() model.Source { return model.NuGet }

// Search implements Adapter.
func (a *NuGet) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// NuGet ANDs every term, strictly enough that two are often still too
	// many: measured live, a 7-term idea returns zero, "command listening"
	// returns zero, and "listening" returns 20. So this one narrows all the
	// way down to a single keyword; the noise that lets in is dropped by
	// similarity ranking.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 1) {
		var body struct {
			Data *[]struct {
				ID             *string `json:"id"`
				Description    string  `json:"description"`
				TotalDownloads *uint64 `json:"totalDownloads"`
			} `json:"data"`
		}
		params := url.Values{"q": {term}, "take": {"20"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/query", params, &body); err != nil {
			return nil, err
		}
		if body.Data == nil {
			return nil, missingField("data")
		}
		matches = matches[:0]
		for _, p := range *body.Data {
			if p.ID == nil {
				return nil, missingField("id")
			}
			if p.Description == "" {
				continue
			}
			matches = append(matches, model.Match{
				Name:        *p.ID,
				Source:      model.NuGet,
				URL:         "https://www.nuget.org/packages/" + *p.ID,
				Description: p.Description,
				Popularity:  p.TotalDownloads,
			})
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}
