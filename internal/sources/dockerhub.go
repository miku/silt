package sources

import (
	"context"
	"net/http"
	"strings"

	"github.com/miku/silt/internal/model"
)

const dockerHubBaseURL = "https://hub.docker.com"

// DockerHub searches Docker Hub repositories: GET /v2/search/repositories/.
//
// The v3 catalog endpoint would carry an updated_at date, but reaching it
// means swapping search endpoints, not reading another field, so Docker Hub
// matches have no date.
type DockerHub struct {
	Client  *http.Client
	BaseURL string
}

// NewDockerHub returns an adapter for the live Docker Hub.
func NewDockerHub(client *http.Client) *DockerHub {
	return &DockerHub{Client: client, BaseURL: dockerHubBaseURL}
}

// ID implements Adapter.
func (*DockerHub) ID() model.Source { return model.DockerHub }

// Search implements Adapter.
func (a *DockerHub) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	var body struct {
		Results *[]struct {
			RepoName         *string `json:"repo_name"`
			ShortDescription string  `json:"short_description"`
			StarCount        *uint64 `json:"star_count"`
		} `json:"results"`
	}
	params := queryParams{{"query", strings.Join(q.Keywords, " ")}, {"page_size", "20"}}
	if err := getJSON(ctx, a.Client, a.BaseURL+"/v2/search/repositories/", params, &body); err != nil {
		return nil, err
	}
	if body.Results == nil {
		return nil, missingField("results")
	}
	var matches []model.Match
	for _, r := range *body.Results {
		if r.RepoName == nil {
			return nil, missingField("repo_name")
		}
		if r.ShortDescription == "" {
			continue
		}
		matches = append(matches, model.Match{
			Name:        *r.RepoName,
			Source:      model.DockerHub,
			URL:         "https://hub.docker.com/r/" + *r.RepoName,
			Description: r.ShortDescription,
			Popularity:  r.StarCount,
		})
	}
	return matches, nil
}
