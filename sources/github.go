package sources

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/miku/silt"
	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

const gitHubBaseURL = "https://api.github.com"

// GitHub searches GitHub repositories: GET /search/repositories.
type GitHub struct {
	Client  *http.Client
	BaseURL string
	// Token is a personal access token, which lifts the rate limit. Empty
	// means unauthenticated.
	Token string
}

// NewGitHub returns an adapter for the live GitHub API, authenticated with
// GITHUB_TOKEN if that is set.
func NewGitHub(client *http.Client) *GitHub {
	return &GitHub{Client: client, BaseURL: gitHubBaseURL, Token: os.Getenv("GITHUB_TOKEN")}
}

// ID implements Adapter.
func (*GitHub) ID() model.Source { return model.GitHub }

// Search implements Adapter.
func (a *GitHub) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// No sort: GitHub then orders by its default "best match" (text
	// relevance) rather than raw stars, so a low-star but on-topic repo isn't
	// buried below the page window by popular-but-less-relevant ones. Ranking
	// does the final semantic ordering; popularity stays an informational
	// signal, not a gate on inclusion. A wider page (50) deepens the candidate
	// pool for the long tail.
	params := url.Values{
		"q":        {strings.Join(q.Keywords, " ") + " in:description,readme"},
		"per_page": {"50"},
	}
	req, err := newRequest(ctx, http.MethodGet, a.BaseURL+"/search/repositories", params, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if a.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
	}
	resp, err := send(a.Client, req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		resp.Body.Close()
		return nil, &silt.ParseError{Msg: "GitHub API returned 401 — check that GITHUB_TOKEN is valid"}
	case http.StatusForbidden:
		resp.Body.Close()
		if a.Token != "" {
			return nil, &silt.ParseError{Msg: "GitHub API rate limit exceeded even with token — try again later"}
		}
		return nil, &silt.ParseError{Msg: "GitHub API rate limit exceeded — set GITHUB_TOKEN env var for higher limits"}
	}
	var body struct {
		Items *[]struct {
			FullName    *string `json:"full_name"`
			Description string  `json:"description"`
			HTMLURL     *string `json:"html_url"`
			Stars       *uint64 `json:"stargazers_count"`
			// Last push to any branch. Preferred over updated_at, which also
			// moves on metadata-only events like a star or a description edit.
			PushedAt freshness.Lenient[string] `json:"pushed_at"`
		} `json:"items"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		return nil, err
	}
	if body.Items == nil {
		return nil, missingField("items")
	}
	var matches []model.Match
	for _, r := range *body.Items {
		switch {
		case r.FullName == nil:
			return nil, missingField("full_name")
		case r.HTMLURL == nil:
			return nil, missingField("html_url")
		case r.Description == "":
			continue
		}
		matches = append(matches, model.Match{
			Name:        *r.FullName,
			Source:      model.GitHub,
			URL:         *r.HTMLURL,
			Description: r.Description,
			Popularity:  r.Stars,
			LastUpdated: rfc3339Date(r.PushedAt),
		})
	}
	return matches, nil
}
