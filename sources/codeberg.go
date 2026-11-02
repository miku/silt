package sources

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

const (
	codebergBaseURL = "https://codeberg.org"
	// Codeberg's search is slow: measured live (2026-11-02), 6–14 s a
	// request for a keyword, whatever the parameters, against well under a
	// second for most sources. The shared client gives a request 10 s and
	// the fan-out a source 15 s, so Codeberg gets its own, for the two
	// requests a search makes at most. It is searched only when asked for.
	codebergRequestTimeout = 30 * time.Second
	codebergSearchTimeout  = 45 * time.Second
)

// Codeberg searches the repositories on Codeberg, a Forgejo forge, through
// Forgejo's API: GET /api/v1/repos/search.
type Codeberg struct {
	Client  *http.Client
	BaseURL string
}

// NewCodeberg returns an adapter for the live Codeberg, with a copy of
// client that waits codebergRequestTimeout for an answer.
func NewCodeberg(client *http.Client) *Codeberg {
	if client != nil {
		c := *client
		c.Timeout = codebergRequestTimeout
		client = &c
	}
	return &Codeberg{Client: client, BaseURL: codebergBaseURL}
}

// ID implements Adapter.
func (*Codeberg) ID() model.Source { return model.Codeberg }

func (*Codeberg) searchTimeout() time.Duration { return codebergSearchTimeout }

// Search implements Adapter.
func (a *Codeberg) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// Forgejo matches q as one phrase, ignoring case, against each name and
	// description: measured live, "kill port" finds two port killers, while
	// "port kill" and "kills process port" find nothing. (A comma ORs
	// phrases, which lets in the most-starred repositories that mention any
	// keyword at all.) Of six realistic ideas, none matched with three
	// keywords or more, two with the two longest and the rest only with the
	// longest alone, so only those two are tried: each request takes
	// seconds. The noise a single keyword lets in is dropped by similarity
	// ranking.
	candidates := narrowingCandidates(q, 1)
	var matches []model.Match
	for _, term := range candidates[max(0, len(candidates)-2):] {
		var err error
		if matches, err = a.search(ctx, term); err != nil {
			return nil, err
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}

// search searches for the phrase term and returns the repositories that
// have a description.
func (a *Codeberg) search(ctx context.Context, term string) ([]model.Match, error) {
	// mode=source leaves out forks, and mirrors, which would bring a GitHub
	// repository back a second time under another URL. A single keyword
	// matches more than the 50 a page holds, so the most-starred come first.
	params := queryParams{
		{"q", term},
		{"includeDesc", "true"},
		{"mode", "source"},
		{"sort", "stars"},
		{"order", "desc"},
		{"limit", "50"},
	}
	var body struct {
		Data *[]struct {
			FullName    *string `json:"full_name"`
			Description string  `json:"description"`
			HTMLURL     *string `json:"html_url"`
			Stars       *uint64 `json:"stars_count"`
			// Any change to the repository, a push or an edit of its
			// settings: Forgejo has no pushed_at.
			UpdatedAt freshness.Lenient[string] `json:"updated_at"`
		} `json:"data"`
	}
	if err := getJSON(ctx, a.Client, a.BaseURL+"/api/v1/repos/search", params, &body); err != nil {
		return nil, err
	}
	if body.Data == nil {
		return nil, missingField("data")
	}
	var matches []model.Match
	for _, r := range *body.Data {
		switch {
		case r.FullName == nil:
			return nil, missingField("full_name")
		case r.HTMLURL == nil:
			return nil, missingField("html_url")
		}
		// Descriptions on Codeberg may run over several lines.
		desc := strings.Join(strings.Fields(r.Description), " ")
		if desc == "" {
			continue
		}
		matches = append(matches, model.Match{
			Name:        *r.FullName,
			Source:      model.Codeberg,
			URL:         *r.HTMLURL,
			Description: desc,
			Popularity:  r.Stars,
			LastUpdated: rfc3339Date(r.UpdatedAt),
		})
	}
	return matches, nil
}
