package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/silt"
	"github.com/miku/silt/internal/sources"
)

func TestPrintChecks(t *testing.T) {
	reset := time.Date(2026, 10, 29, 21, 38, 53, 0, time.Local)
	var b strings.Builder
	printChecks(&b, []sources.SourceCheck{
		{Source: model.CratesIO, Status: sources.CheckReached, Elapsed: 412 * time.Millisecond, Matches: 20},
		{Source: model.GitHub, Status: sources.CheckRateLimited, Elapsed: 300 * time.Millisecond,
			RateLimit: sources.RateLimit{Source: model.GitHub, Reset: reset, Hint: "set GITHUB_TOKEN for a higher limit"}},
		{Source: model.PyPI, Status: sources.CheckBrowsed, Elapsed: 3100 * time.Millisecond, Matches: 1},
		{Source: model.Homebrew, Status: sources.CheckByName, Elapsed: time.Second, Matches: 3},
		{Source: model.JetBrains, Status: sources.CheckWalled, Elapsed: 200 * time.Millisecond, Err: &silt.UnavailableError{Msg: "the search is bot-walled"}},
		{Source: model.Go, Status: sources.CheckTimedOut, Elapsed: 15 * time.Second},
		{Source: model.DockerHub, Status: sources.CheckFailed, Elapsed: 600 * time.Millisecond, Err: errors.New("http request failed: HTTP status 503")},
	})
	want := `  crates.io      0.4s  reached, 20 matches
  GitHub         0.3s  rate-limited until 21:39; set GITHUB_TOKEN for a higher limit
  PyPI           3.1s  search walled; reached through CloakBrowser, 1 match
  Homebrew       1.0s  search walled; reached by names only, 3 matches
  JetBrains      0.2s  walled: the search is bot-walled
  Go            15.0s  timed out
  Docker Hub     0.6s  failed: http request failed: HTTP status 503
3 of 7 sources reached.
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}
