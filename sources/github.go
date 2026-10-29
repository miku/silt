package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

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

// cacheKey tells results found with different tokens apart, since a token
// widens the search to the private repositories it can see. The token itself
// is not stored.
func (a *GitHub) cacheKey() string {
	if a.Token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(a.Token))
	return hex.EncodeToString(sum[:8])
}

// Search implements Adapter.
func (a *GitHub) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// No sort: GitHub then orders by its default "best match" (text
	// relevance) rather than raw stars, so a low-star but on-topic repo isn't
	// buried below the page window by popular-but-less-relevant ones. Ranking
	// does the final semantic ordering; popularity stays an informational
	// signal, not a gate on inclusion. A wider page (50) deepens the candidate
	// pool for the long tail.
	params := queryParams{
		{"q", strings.Join(q.Keywords, " ") + " in:description,readme"},
		{"per_page", "50"},
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
	case http.StatusForbidden, http.StatusTooManyRequests:
		// GitHub answers either for its primary and its secondary rate limits.
		resp.Body.Close()
		limited := &rateLimitError{reset: gitHubReset(resp.Header, time.Now())}
		if a.Token != "" {
			limited.err = &silt.ParseError{Msg: "GitHub API rate limit exceeded even with token — try again later"}
		} else {
			limited.err = &silt.ParseError{Msg: "GitHub API rate limit exceeded — set GITHUB_TOKEN env var for higher limits"}
			limited.hint = "set GITHUB_TOKEN for a higher limit"
		}
		return nil, limited
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

// gitHubReset returns when GitHub's rate limit lifts: what Retry-After says,
// which GitHub sends for its secondary limits, or else X-RateLimit-Reset (in
// seconds since the epoch) once X-RateLimit-Remaining is 0. It is zero if
// GitHub says neither.
func gitHubReset(h http.Header, now time.Time) time.Time {
	if t := retryAfter(h, now); !t.IsZero() {
		return t
	}
	if h.Get("X-RateLimit-Remaining") != "0" {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}
