package sources

import (
	"context"
	"net/http"

	"github.com/miku/silt/internal/freshness"
	"github.com/miku/silt/internal/model"
)

const mavenBaseURL = "https://search.maven.org"

// Maven searches Maven Central's Solr index: GET /solrsearch/select?q=.
type Maven struct {
	Client  *http.Client
	BaseURL string
}

// NewMaven returns an adapter for the live Maven Central search.
func NewMaven(client *http.Client) *Maven { return &Maven{Client: client, BaseURL: mavenBaseURL} }

// ID implements Adapter.
func (*Maven) ID() model.Source { return model.Maven }

// Search implements Adapter.
//
// Maven Central's response has no download, star or rank field comparable to
// the other sources' popularity signals (versionCount is not one), so
// popularity is always nil.
func (a *Maven) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// Central's Solr index ANDs every term against artifact coordinates only,
	// which is far stricter than the other registries: measured live, "kill
	// process port" returns zero and even 2 terms return zero, while
	// "process" alone returns 845. Maven is the one source that has to narrow
	// all the way down to a single keyword to come back with anything; the
	// noise that lets in is dropped by similarity ranking.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 1) {
		var body struct {
			Response *struct {
				Docs *[]struct {
					G string `json:"g"`
					A string `json:"a"`
					// Publish time of latestVersion, in epoch milliseconds.
					Timestamp freshness.Lenient[int64] `json:"timestamp"`
				} `json:"docs"`
			} `json:"response"`
		}
		params := queryParams{{"q", term}, {"rows", "20"}, {"wt", "json"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/solrsearch/select", params, &body); err != nil {
			return nil, err
		}
		switch {
		case body.Response == nil:
			return nil, missingField("response")
		case body.Response.Docs == nil:
			return nil, missingField("docs")
		}
		matches = matches[:0]
		for _, d := range *body.Response.Docs {
			m := model.Match{
				Name:        d.G + ":" + d.A,
				Source:      model.Maven,
				URL:         "https://central.sonatype.com/artifact/" + d.G + "/" + d.A,
				Description: d.A,
			}
			if d.Timestamp.OK {
				if s, ok := freshness.FromUnixMillis(d.Timestamp.V); ok {
					m.LastUpdated = &s
				}
			}
			matches = append(matches, m)
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}
