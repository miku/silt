package sources

import (
	"context"
	"strings"
	"testing"

	"github.com/miku/silt/model"
)

const gitHubBody = `{
  "total_count": 2,
  "items": [
    {
      "full_name": "tokio-rs/tokio",
      "pushed_at": "2026-07-31T18:49:43Z",
      "description": "An async runtime for Rust.",
      "html_url": "https://github.com/tokio-rs/tokio",
      "stargazers_count": 27000
    },
    {
      "full_name": "async-rs/async-std",
      "description": "Async std library.",
      "html_url": "https://github.com/async-rs/async-std",
      "stargazers_count": 4000
    }
  ]
}`

func gitHubFor(srv *mockServer) *GitHub {
	return &GitHub{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func gitHubServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/search/repositories", respond(status, body)))
}

func TestGitHubIDIsGitHub(t *testing.T) {
	if got := NewGitHub(nil).ID(); got != model.GitHub {
		t.Fatalf("ID() = %v", got)
	}
}

func TestNewGitHubReadsTheTokenFromTheEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	if got := NewGitHub(nil).Token; got != "ghp_test" {
		t.Errorf("Token = %q", got)
	}
	t.Setenv("GITHUB_TOKEN", "")
	if got := NewGitHub(nil).Token; got != "" {
		t.Errorf("an empty GITHUB_TOKEN must mean no token, got %q", got)
	}
}

func TestGitHubMapsRepositoriesIntoMatches(t *testing.T) {
	srv := gitHubServer(t, 200, gitHubBody)
	matches, err := gitHubFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	first := matches[0]
	if first.Name != "tokio-rs/tokio" || first.Source != model.GitHub ||
		first.URL != "https://github.com/tokio-rs/tokio" ||
		first.Description != "An async runtime for Rust." ||
		deref(first.Popularity) != uint64(27000) || first.Similarity != 0 {
		t.Errorf("first = %+v", first)
	}
}

func TestGitHubSendsQueryAndUserAgent(t *testing.T) {
	srv := gitHubServer(t, 200, gitHubBody)
	if _, err := gitHubFor(srv).Search(context.Background(), query()); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	params := reqs[0].URL.Query()
	if got := params.Get("q"); got != "async runtime in:description,readme" {
		t.Errorf("q = %q", got)
	}
	// #31: no sort param (GitHub defaults to best-match relevance, not raw
	// stars) and a wider page so low-star on-topic repos can surface.
	if params.Has("sort") {
		t.Error("sort must not be sent")
	}
	if got := params.Get("per_page"); got != "50" {
		t.Errorf("per_page = %q", got)
	}
	if reqs[0].UserAgent() == "" {
		t.Error("no User-Agent sent")
	}
	if got := reqs[0].Header.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q", got)
	}
	if got := reqs[0].Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q without a token", got)
	}
}

func TestGitHubSendsTheTokenAsABearer(t *testing.T) {
	srv := gitHubServer(t, 200, gitHubBody)
	a := gitHubFor(srv)
	a.Token = "ghp_test"
	if _, err := a.Search(context.Background(), query()); err != nil {
		t.Fatal(err)
	}
	if got := srv.Requests()[0].Header.Get("Authorization"); got != "Bearer ghp_test" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestGitHubNullDescriptionIsFilteredOut(t *testing.T) {
	srv := gitHubServer(t, 200, `{"total_count": 2, "items": [
		{"full_name": "x/y", "description": null, "html_url": "https://github.com/x/y", "stargazers_count": 1},
		{"full_name": "x/z", "description": "", "html_url": "https://github.com/x/z", "stargazers_count": 1}
	]}`)
	matches, err := gitHubFor(srv).Search(context.Background(), query())
	if err != nil || len(matches) != 0 {
		t.Errorf("Search = %+v, %v; want no matches", matches, err)
	}
}

func TestGitHubEmptyResultsIsOK(t *testing.T) {
	srv := gitHubServer(t, 200, `{"total_count": 0, "items": []}`)
	matches, err := gitHubFor(srv).Search(context.Background(), query())
	if err != nil || len(matches) != 0 {
		t.Errorf("Search = %+v, %v; want no matches", matches, err)
	}
}

func TestGitHubServerErrorIsPropagated(t *testing.T) {
	srv := gitHubServer(t, 503, "")
	if _, err := gitHubFor(srv).Search(context.Background(), query()); err == nil {
		t.Error("a 5xx response must surface as an error")
	}
}

func TestGitHubMalformedBodyIsPropagated(t *testing.T) {
	for _, body := range []string{"<html>not json</html>", `{"total_count": 0}`, `{"items": [{"full_name": "x/y", "description": "d"}]}`} {
		srv := gitHubServer(t, 200, body)
		if _, err := gitHubFor(srv).Search(context.Background(), query()); err == nil {
			t.Errorf("%s: an unparseable body must surface as an error", body)
		}
	}
}

func TestGitHub401WithTokenReturnsAuthError(t *testing.T) {
	srv := gitHubServer(t, 401, "")
	a := gitHubFor(srv)
	a.Token = "bad-token"
	_, err := a.Search(context.Background(), query())
	if err == nil {
		t.Fatal("want an error")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "401") && !strings.Contains(msg, "token") && !strings.Contains(msg, "invalid") {
		t.Errorf("expected auth error, got: %v", err)
	}
}

func TestGitHub403WithTokenReturnsRateLimitError(t *testing.T) {
	srv := gitHubServer(t, 403, "")
	a := gitHubFor(srv)
	a.Token = "valid-token"
	_, err := a.Search(context.Background(), query())
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate limit") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "set GITHUB_TOKEN") {
		t.Errorf("with a token set, the hint to set one is wrong: %v", err)
	}
}

func TestGitHub403WithoutTokenReturnsRateLimitHint(t *testing.T) {
	srv := gitHubServer(t, 403, "")
	_, err := gitHubFor(srv).Search(context.Background(), query())
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("expected GITHUB_TOKEN hint, got: %v", err)
	}
}

func TestGitHubUsesPushedAtForLastUpdated(t *testing.T) {
	srv := gitHubServer(t, 200, gitHubBody)
	m, err := gitHubFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if got := deref(m[0].LastUpdated); got != "2026-07-31T18:49:43Z" {
		t.Errorf("last_updated = %v", got)
	}
	if m[1].LastUpdated != nil {
		t.Errorf("last_updated = %v, want nil", deref(m[1].LastUpdated))
	}
}

func TestGitHubMalformedDateNeverFailsTheSource(t *testing.T) {
	for _, date := range []string{`""`, `1785523783`, `null`} {
		body := strings.Replace(gitHubBody, `"2026-07-31T18:49:43Z"`, date, 1)
		srv := gitHubServer(t, 200, body)
		m, err := gitHubFor(srv).Search(context.Background(), query())
		if err != nil {
			t.Errorf("pushed_at %s failed the source: %v", date, err)
			continue
		}
		if len(m) != 2 || m[0].LastUpdated != nil {
			t.Errorf("pushed_at %s: matches = %+v", date, m)
		}
	}
}
