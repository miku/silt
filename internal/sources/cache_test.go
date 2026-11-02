package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/silt"
)

// staticSource counts its searches and always returns matches.
type staticSource struct {
	id      model.Source
	matches []model.Match
	calls   atomic.Int32
}

func (s *staticSource) ID() model.Source { return s.id }
func (s *staticSource) Search(context.Context, model.Query) ([]model.Match, error) {
	s.calls.Add(1)
	return s.matches, nil
}

func testCache(t *testing.T) *Cache {
	return &Cache{Dir: filepath.Join(t.TempDir(), "results"), TTL: time.Hour}
}

func cachedFanout(c *Cache) fanout {
	f := fastFanout
	f.cache = c
	return f
}

var cacheQuery = model.Query{Idea: "async runtime for rust", Keywords: []string{"async", "runtime", "rust"}}

func TestCacheAnswersARepeatedSearch(t *testing.T) {
	pop, updated := uint64(950000000), "2026-05-15T06:13:41Z"
	src := &staticSource{id: model.CratesIO, matches: []model.Match{
		{Name: "tokio", Source: model.CratesIO, URL: "https://crates.io/crates/tokio", Description: "An event-driven platform.",
			Popularity: &pop, LastUpdated: &updated},
		{Name: "smol", Source: model.CratesIO, URL: "https://crates.io/crates/smol"},
	}}
	f := cachedFanout(testCache(t))

	first := f.search(t.Context(), []Adapter{src}, cacheQuery)
	if len(first.Cached) != 0 {
		t.Errorf("first search: cached %v, want none", first.Cached)
	}
	second := f.search(t.Context(), []Adapter{src}, cacheQuery)
	if n := src.calls.Load(); n != 1 {
		t.Errorf("searched the source %d times, want 1", n)
	}
	if want := []model.Source{model.CratesIO}; !reflect.DeepEqual(second.Cached, want) || !reflect.DeepEqual(second.Reached, want) {
		t.Errorf("second search: cached %v, reached %v, want %v for both", second.Cached, second.Reached, want)
	}
	if !reflect.DeepEqual(second.Matches, first.Matches) {
		t.Errorf("cached matches differ:\n got %+v\nwant %+v", second.Matches, first.Matches)
	}
}

func TestCacheNeverKeepsAFailure(t *testing.T) {
	c := testCache(t)
	src := &countingSource{err: &silt.UnavailableError{Msg: "walled"}}
	f := cachedFanout(c)
	for range 2 {
		if out := f.search(t.Context(), []Adapter{src}, cacheQuery); len(out.Failed) != 1 || len(out.Cached) != 0 {
			t.Fatalf("failed %v, cached %v; want the source failed, not cached", out.Failed, out.Cached)
		}
	}
	if n := src.calls.Load(); n != 2 {
		t.Errorf("searched the source %d times, want 2: a failure must be retried on the next run", n)
	}
	if entries, _ := os.ReadDir(c.Dir); len(entries) != 0 {
		t.Errorf("the cache holds %d files after failures only", len(entries))
	}
}

func TestCacheKeepsAnEmptyResult(t *testing.T) {
	src := &staticSource{id: model.Go}
	f := cachedFanout(testCache(t))
	f.search(t.Context(), []Adapter{src}, cacheQuery)
	out := f.search(t.Context(), []Adapter{src}, cacheQuery)
	if n := src.calls.Load(); n != 1 || len(out.Cached) != 1 {
		t.Errorf("calls %d, cached %v; a genuinely empty result is a success and should be kept", n, out.Cached)
	}
}

func TestCacheEntriesExpireAndArePruned(t *testing.T) {
	c := testCache(t)
	src := &staticSource{id: model.Npm, matches: []model.Match{{Name: "kill-port", Source: model.Npm, URL: "https://x/kill-port"}}}
	c.put(src, cacheQuery, src.matches)
	if _, ok := c.get(src, cacheQuery); !ok {
		t.Fatal("a fresh entry should be a hit")
	}
	old := time.Now().Add(-c.TTL - time.Minute)
	path := c.path(src, cacheQuery)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.get(src, cacheQuery); ok {
		t.Error("an entry older than the TTL should be a miss")
	}

	// prune removes expired entries and stale temporary files, and nothing else.
	fresh := &staticSource{id: model.GitHub}
	c.put(fresh, cacheQuery, nil)
	tmp, other := filepath.Join(c.Dir, "leftover.tmp"), filepath.Join(c.Dir, "notes.txt")
	for _, p := range []string{tmp, other} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	c.prune()
	var left []string
	entries, _ := os.ReadDir(c.Dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if want := []string{filepath.Base(c.path(fresh, cacheQuery)), "notes.txt"}; !reflect.DeepEqual(left, want) {
		t.Errorf("after prune: %v, want %v", left, want)
	}
}

func TestCacheKeysOnSourceQueryAndToken(t *testing.T) {
	c := testCache(t)
	crates := &staticSource{id: model.CratesIO}
	base := c.path(crates, cacheQuery)
	for name, path := range map[string]string{
		"source":   c.path(&staticSource{id: model.Npm}, cacheQuery),
		"idea":     c.path(crates, model.Query{Idea: "async runtime for rust!", Keywords: cacheQuery.Keywords}),
		"keywords": c.path(crates, model.Query{Idea: cacheQuery.Idea, Keywords: []string{"async", "runtime"}}),
	} {
		if path == base {
			t.Errorf("a different %s gives the same entry", name)
		}
	}

	noToken, withToken := &GitHub{}, &GitHub{Token: "ghp_secret"}
	if c.path(noToken, cacheQuery) == c.path(withToken, cacheQuery) {
		t.Error("results found with and without a GitHub token share an entry")
	}
	c.put(withToken, cacheQuery, nil)
	b, err := os.ReadFile(c.path(withToken, cacheQuery))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "ghp_secret") || strings.Contains(filepath.Base(c.path(withToken, cacheQuery)), "ghp_secret") {
		t.Error("the token itself ends up in the cache")
	}
}

func TestCacheIgnoresAnUnreadableEntry(t *testing.T) {
	c := testCache(t)
	src := &staticSource{id: model.Hex, matches: []model.Match{{Name: "cachex", Source: model.Hex, URL: "https://hex.pm/packages/cachex"}}}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.path(src, cacheQuery), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := cachedFanout(c)
	if out := f.search(t.Context(), []Adapter{src}, cacheQuery); len(out.Cached) != 0 || len(out.Matches) != 1 {
		t.Fatalf("a corrupt entry should be a miss: cached %v, %d matches", out.Cached, len(out.Matches))
	}
	// The search that followed replaced it.
	if out := f.search(t.Context(), []Adapter{src}, cacheQuery); len(out.Cached) != 1 || src.calls.Load() != 1 {
		t.Errorf("cached %v after %d calls; the corrupt entry should have been replaced", out.Cached, src.calls.Load())
	}
}

func TestNilOrZeroTTLCacheCachesNothing(t *testing.T) {
	for _, c := range []*Cache{nil, {Dir: t.TempDir(), TTL: 0}, {Dir: t.TempDir(), TTL: -time.Hour}} {
		src := &staticSource{id: model.AUR}
		f := cachedFanout(c)
		for range 2 {
			f.search(t.Context(), []Adapter{src}, cacheQuery)
		}
		if n := src.calls.Load(); n != 2 {
			t.Errorf("%+v: searched %d times, want 2", c, n)
		}
		c.prune() // must not panic on nil
	}
}

// roundTripFunc is an http.RoundTripper made of a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSearcherAnswersARepeatedSearchFromItsCache(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(twoCrateBody)),
			Request:    req,
		}, nil
	})}
	s := Searcher{Client: client, Cache: testCache(t)}
	include := []model.Source{model.CratesIO}

	first := s.SearchFiltered(t.Context(), cacheQuery, include, nil)
	sent := requests.Load()
	if sent == 0 || len(first.Matches) != 2 || len(first.Cached) != 0 {
		t.Fatalf("first search: %d requests, %d matches, cached %v", sent, len(first.Matches), first.Cached)
	}
	second := s.SearchFiltered(t.Context(), cacheQuery, include, nil)
	if n := requests.Load(); n != sent {
		t.Errorf("the repeated search sent %d more requests", n-sent)
	}
	if !reflect.DeepEqual(second.Cached, include) || !reflect.DeepEqual(second.Matches, first.Matches) {
		t.Errorf("second search: cached %v, matches %+v", second.Cached, second.Matches)
	}
}

// staleCache is a cache with a 1 h TTL and a day of stale-if-error, holding
// matches for src and q, written age ago.
func staleCache(t *testing.T, src Adapter, age time.Duration) *Cache {
	t.Helper()
	c := &Cache{Dir: filepath.Join(t.TempDir(), "results"), TTL: time.Hour, StaleIfError: 24 * time.Hour}
	c.put(src, cacheQuery, []model.Match{{Name: "old-match", Source: src.ID(), URL: "https://x/old"}})
	when := time.Now().Add(-age)
	if err := os.Chtimes(c.path(src, cacheQuery), when, when); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAnOlderResultStandsInForAFailedSearch(t *testing.T) {
	src := &countingSource{err: &silt.HTTPError{StatusCode: 503, Err: errors.New("down")}}
	out := cachedFanout(staleCache(t, src, 5*time.Hour)).search(t.Context(), []Adapter{src}, cacheQuery)
	if n := src.calls.Load(); n != 2 {
		t.Errorf("searched %d times; the entry is past its TTL, so the source must be searched (and retried)", n)
	}
	if len(out.Failed) != 0 || !reflect.DeepEqual(out.Reached, []model.Source{model.PyPI}) || len(out.Cached) != 0 ||
		len(out.Matches) != 1 || out.Matches[0].Name != "old-match" {
		t.Fatalf("failed %v, reached %v, cached %v, matches %+v", out.Failed, out.Reached, out.Cached, out.Matches)
	}
	if len(out.Stale) != 1 || out.Stale[0].Source != model.PyPI || out.Stale[0].Age < 5*time.Hour || out.Stale[0].Age > 5*time.Hour+time.Minute {
		t.Errorf("stale %+v; want PyPI, 5h old", out.Stale)
	}
}

func TestAnOlderResultIsNotUsedPastStaleIfError(t *testing.T) {
	src := &countingSource{err: &silt.UnavailableError{Msg: "walled"}}
	for _, c := range []*Cache{
		staleCache(t, src, 25*time.Hour),
		func() *Cache { c := staleCache(t, src, 5*time.Hour); c.StaleIfError = 0; return c }(),
	} {
		out := cachedFanout(c).search(t.Context(), []Adapter{src}, cacheQuery)
		if !reflect.DeepEqual(out.Failed, []model.Source{model.PyPI}) || len(out.Stale) != 0 || len(out.Matches) != 0 {
			t.Errorf("StaleIfError %v: failed %v, stale %v", c.StaleIfError, out.Failed, out.Stale)
		}
	}
}

func TestAnOlderResultNeverReplacesASuccess(t *testing.T) {
	src := &staticSource{id: model.PyPI, matches: []model.Match{{Name: "new-match", Source: model.PyPI, URL: "https://x/new"}}}
	c := staleCache(t, src, 5*time.Hour)
	out := cachedFanout(c).search(t.Context(), []Adapter{src}, cacheQuery)
	if len(out.Stale) != 0 || len(out.Matches) != 1 || out.Matches[0].Name != "new-match" {
		t.Errorf("stale %v, matches %+v", out.Stale, out.Matches)
	}
	if fi, err := os.Stat(c.path(src, cacheQuery)); err != nil || time.Since(fi.ModTime()) > time.Minute {
		t.Errorf("the success should have replaced the old entry: %v", err)
	}
}

func TestACancelledSearchUsesNoOlderResult(t *testing.T) {
	src := &countingSource{err: context.Canceled}
	out := cachedFanout(staleCache(t, src, 5*time.Hour)).search(t.Context(), []Adapter{src}, cacheQuery)
	if len(out.Stale) != 0 || len(out.Failed) != 1 {
		t.Errorf("stale %v, failed %v", out.Stale, out.Failed)
	}
}

func TestARateLimitedSourceCanStandInAndStillSaysSo(t *testing.T) {
	src := &countingSource{err: &rateLimitError{err: &silt.ParseError{Msg: "limited"}, reset: time.Now().Add(time.Hour), hint: "set a token"}}
	out := cachedFanout(staleCache(t, src, 3*time.Hour)).search(t.Context(), []Adapter{src}, cacheQuery)
	if len(out.Stale) != 1 || len(out.RateLimited) != 1 || out.RateLimited[0].Hint != "set a token" || len(out.Failed) != 0 {
		t.Errorf("stale %v, rate-limited %v, failed %v", out.Stale, out.RateLimited, out.Failed)
	}
}

func TestPruneKeepsEntriesForStaleIfError(t *testing.T) {
	src := &staticSource{id: model.Npm}
	other := &staticSource{id: model.Go}
	c := staleCache(t, src, 5*time.Hour)
	c.put(other, cacheQuery, nil)
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(c.path(other, cacheQuery), old, old); err != nil {
		t.Fatal(err)
	}
	c.prune()
	if _, err := os.Stat(c.path(src, cacheQuery)); err != nil {
		t.Errorf("a 5h-old entry was pruned, but can still stand in: %v", err)
	}
	if _, err := os.Stat(c.path(other, cacheQuery)); err == nil {
		t.Error("a 25h-old entry was kept")
	}
}
