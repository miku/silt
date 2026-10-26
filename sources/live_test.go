//go:build live

// Live smoke tests — the only tests that hit real registry APIs. They run
// only with -tags live, so go test ./... stays hermetic and offline.
//
// They catch the one thing the mocked tests can't: an upstream API silently
// changing its response shape, which would leave the mocked tests green while
// the real tool returns nothing. Each query uses a deliberately popular,
// stable keyword for its ecosystem, so an empty result means the adapter can
// no longer parse the live response — not that the query matched nothing.

package sources

import (
	"context"
	"strings"
	"testing"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

func liveQuery(idea string, keywords ...string) model.Query {
	return model.Query{Idea: idea, Keywords: keywords}
}

// assertLive checks that a live search for a popular term returned
// well-formed matches attributed to the expected source.
func assertLive(t *testing.T, matches []model.Match, want model.Source) {
	t.Helper()
	if len(matches) == 0 {
		t.Fatalf("%v returned zero matches for a popular query — the live API shape has likely "+
			"drifted and the adapter can no longer parse it", want)
	}
	for _, m := range matches {
		if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.URL) == "" || m.Source != want {
			t.Errorf("%v: malformed match %+v", want, m)
		}
	}
	t.Logf("✓ %v: %d matches", want, len(matches))
}

// assertDated checks the dates of a source that publishes one. A registry
// switching epoch milliseconds to seconds would not error; it would quietly
// date every match to 1970 and render the whole source as abandoned.
func assertDated(t *testing.T, matches []model.Match, want model.Source) {
	t.Helper()
	dated := 0
	thisYear := freshness.Now().Year()
	for _, m := range matches {
		if m.LastUpdated == nil {
			continue
		}
		dated++
		ts, ok := freshness.Parse(*m.LastUpdated)
		if !ok {
			t.Errorf("%v: stored an unparseable date: %q", want, *m.LastUpdated)
		} else if ts.Year() < 2000 || ts.Year() > thisYear+1 {
			t.Errorf("%v: implausible last-updated date %q — the upstream timestamp units have likely changed", want, *m.LastUpdated)
		}
	}
	if dated == 0 {
		t.Errorf("%v: not one of %d live matches carried a last-updated date — the upstream date "+
			"field has likely been renamed, retyped, or dropped", want, len(matches))
	}
	t.Logf("✓ %v: %d/%d matches dated", want, dated, len(matches))
}

func TestLiveCratesIO(t *testing.T) {
	m, err := NewCratesIO(NewHTTPClient()).Search(context.Background(), liveQuery("a fast async runtime for rust", "async", "runtime"))
	if err != nil {
		t.Fatalf("crates.io live search errored: %v", err)
	}
	assertLive(t, m, model.CratesIO)
	assertDated(t, m, model.CratesIO)
}

func TestLiveGitHub(t *testing.T) {
	m, err := NewGitHub(NewHTTPClient()).Search(context.Background(), liveQuery("a kubernetes command line tool", "kubernetes", "cli"))
	if err != nil {
		t.Fatalf("GitHub live search errored: %v", err)
	}
	assertLive(t, m, model.GitHub)
	assertDated(t, m, model.GitHub)
}

func TestLiveHackerNews(t *testing.T) {
	m, err := NewHackerNews(NewHTTPClient()).Search(context.Background(), liveQuery("database", "database"))
	if err != nil {
		t.Fatalf("Hacker News live search errored: %v", err)
	}
	assertLive(t, m, model.HackerNews)
}
