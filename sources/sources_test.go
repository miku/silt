package sources

import (
	"context"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

func TestNarrowedPicksLongestButPreservesOriginalOrder(t *testing.T) {
	// Lengths: ide=3, code=4, spell=5, syntax=6, mistakes=8. The 3 longest
	// are mistakes/syntax/spell, but the result must come back in their
	// original (index) order, not length order.
	keywords := []string{"ide", "code", "spell", "syntax", "mistakes"}
	if got := narrowed(keywords, 3); got != "spell syntax mistakes" {
		t.Errorf("narrowed(3) = %q", got)
	}
	if got := narrowed(keywords, 2); got != "syntax mistakes" {
		t.Errorf("narrowed(2) = %q", got)
	}
}

func TestNarrowedBreaksLengthTiesByOriginalOrder(t *testing.T) {
	// Rust's sort_by is stable, so equal-length keywords keep their order.
	keywords := []string{"aaaa", "bbbb", "cc", "dddd"}
	if got := narrowed(keywords, 2); got != "aaaa bbbb" {
		t.Errorf("narrowed = %q, want the first two of the tied longest", got)
	}
}

func TestNarrowedIsANoOpWhenNCoversAllKeywords(t *testing.T) {
	keywords := []string{"async", "runtime"}
	for _, n := range []int{3, 2} {
		if got := narrowed(keywords, n); got != "async runtime" {
			t.Errorf("narrowed(%d) = %q", n, got)
		}
	}
}

func TestNarrowedOfEmptyKeywordsIsEmpty(t *testing.T) {
	if got := narrowed(nil, 3); got != "" {
		t.Errorf("narrowed = %q", got)
	}
}

func TestCandidatesGoFromTheFullSetDownToMinTerms(t *testing.T) {
	q := model.Query{Idea: "x", Keywords: []string{"ide", "code", "spell", "syntax", "mistakes"}}
	want := []string{"ide code spell syntax mistakes", "spell syntax mistakes", "syntax mistakes"}
	if got := narrowingCandidates(q, 2); !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %q, want %q", got, want)
	}
	// Maven only recovers at a single term, so it asks for one more step.
	if got := narrowingCandidates(q, 1); got[len(got)-1] != "mistakes" {
		t.Errorf("min_terms = 1 must end on the single longest keyword, got %q", got)
	}
}

func TestCandidatesDropNarrowingsThatRepeatAnEarlierQuery(t *testing.T) {
	// With <= 3 keywords the "3 longest" step is the full set again.
	q := model.Query{Idea: "x", Keywords: []string{"async", "runtime"}}
	if got := narrowingCandidates(q, 2); !reflect.DeepEqual(got, []string{"async runtime"}) {
		t.Errorf("candidates = %q", got)
	}
}

func TestCandidatesFallBackToTheIdeaWhenThereAreNoKeywords(t *testing.T) {
	// An empty search param would be sent otherwise.
	q := model.Query{Idea: "kill the process on a port"}
	if got := narrowingCandidates(q, 2); !reflect.DeepEqual(got, []string{"kill the process on a port"}) {
		t.Errorf("candidates = %q", got)
	}
}

func TestIdeaContainsRespectsWordBoundaries(t *testing.T) {
	if !ideaContains("a fast async runtime", []string{"async"}) {
		t.Error("async not found")
	}
	// Substrings inside larger words must not match.
	for _, tc := range []struct{ idea, term string }{
		{"rainbow trains", "ai"},
		{"googol", "go"},
		{"django framework", "go"},
	} {
		if ideaContains(tc.idea, []string{tc.term}) {
			t.Errorf("%q matched %q", tc.idea, tc.term)
		}
	}
}

func TestIdeaContainsChecksAllOccurrencesNotJustTheFirst(t *testing.T) {
	// A non-boundary substring earlier in the string must not mask a later
	// standalone occurrence of the keyword.
	for _, tc := range []struct{ idea, term string }{
		{"a tool for cargo packages written in go", "go"},
		{"email summarizer that uses ai", "ai"},
		{"a good way to go fast", "go"},
	} {
		if !ideaContains(tc.idea, []string{tc.term}) {
			t.Errorf("%q did not match %q", tc.idea, tc.term)
		}
	}
}

func TestIdeaContainsHandlesMultiWordAndPunctuatedTerms(t *testing.T) {
	for _, tc := range []struct {
		idea, term string
		want       bool
	}{
		{"a Machine Learning pipeline", "machine learning", true},
		{"a C# linter", "c#", true},
		{"tooling for .NET apps", ".net", true},
		{"a planet tracker", ".net", false},
		{"a cloud-native operator", "cloud-native", true},
		{"tools for c#7", "c#", false}, // '7' is alphanumeric
		{"ein Werkzeug für go", "go", true},
		{"görüntü", "go", false}, // ö lowercases to non-ASCII, ending no word
	} {
		if got := ideaContains(tc.idea, []string{tc.term}); got != tc.want {
			t.Errorf("ideaContains(%q, %q) = %v, want %v", tc.idea, tc.term, got, tc.want)
		}
	}
}

func TestGitHubAndHackerNewsAreAlwaysSelected(t *testing.T) {
	// Whatever the idea, the two language-agnostic indexes are present so
	// they are never falsely advertised but unreachable.
	for _, idea := range []string{"a ruby gem for parsing csv", "asdf qwer zxcv", "rust crate"} {
		s := detectSources(idea)
		if !s[model.GitHub] || !s[model.HackerNews] {
			t.Errorf("%q: selected %v", idea, s)
		}
	}
}

func TestEveryBuiltSourceIsReachableFromSomeIdea(t *testing.T) {
	// Guards against re-introducing a "marketed but never selected" source:
	// each source an adapter can be built for must be selectable.
	ideas := []string{
		"rust crate for embedded firmware",
		"a python pandas data pipeline",
		"a typescript react frontend component",
		"a golang microservice",
		"a java spring boot service",
		"a ruby on rails gem",
		"a c# dotnet unity game",
		"a docker container for kubernetes",
		"a vscode extension for editors",
		"a macos homebrew tool",
		"a php composer package for laravel",
		"an elixir phoenix library for caching",
		"a helm chart to deploy a kubernetes operator",
		"a pacman helper for installing arch linux aur packages",
		"a haskell cabal library for parsing",
		"a nix flake for building containers",
		"anything at all with no signal",
	}
	seen := make(map[model.Source]bool)
	for _, idea := range ideas {
		for s := range detectSources(idea) {
			seen[s] = true
		}
	}
	for _, s := range model.All() {
		if !seen[s] {
			t.Errorf("%v is built but never selected by detectSources", s)
		}
	}
}

func TestLanguageMentionsSelectTheirRegistry(t *testing.T) {
	for idea, want := range map[string]model.Source{
		"a rust crate for parsing":     model.CratesIO,
		"a python library for parsing": model.PyPI,
		"a docker image for caching":   model.DockerHub,
		"a ruby gem for parsing":       model.RubyGems,
	} {
		if !detectSources(idea)[want] {
			t.Errorf("%q did not select %v", idea, want)
		}
	}
}

func TestGoAndAIMatchNaturalPhrasings(t *testing.T) {
	// Regression: trailing-space keywords ("go ", "ai ") used to be
	// impossible to match. These phrasings deliberately avoid the "cli" rule
	// (which would add Go on its own) so they isolate the keyword.
	if !detectSources("a fast Go library for parsing json")[model.Go] {
		t.Error("Go not selected")
	}
	if !detectSources("a library that uses AI to summarize text")[model.PyPI] {
		t.Error("PyPI not selected")
	}
	// And the keyword must still win when a non-boundary substring precedes
	// the standalone word (the first-occurrence regression).
	if !detectSources("a cargo workspace tool also written in go")[model.Go] {
		t.Error("Go not selected after cargo")
	}
}

func TestPortKillerDemoSearchesNpm(t *testing.T) {
	// The flagship README example must reach npm, where fkill-cli / kill-port
	// live — otherwise the headline demo finds no prior art.
	for _, idea := range []string{
		"interactive cli to kill whatever's on a port",
		"CLI tool that kills whatever's on a port",
	} {
		if s := detectSources(idea); !s[model.Npm] {
			t.Errorf("npm missing for %q: %v", idea, s)
		}
	}
}

func TestJetBrainsIDENamesSelectJetBrainsEvenWithoutIDEKeyword(t *testing.T) {
	// Guards the dedicated IDE-name rule specifically: these ideas name a
	// JetBrains product but avoid "ide"/"plugin"/"editor", which would
	// otherwise select JetBrains via the vscode/ide rule instead and mask
	// this rule being deleted.
	for _, idea := range []string{
		"a pycharm helper for databases",
		"a rider tool for unit testing",
		"a clion helper for cmake projects",
	} {
		if s := detectSources(idea); !s[model.JetBrains] {
			t.Errorf("JetBrains missing for %q: %v", idea, s)
		}
	}
}

func TestNoSignalFallsBackToBroadSweep(t *testing.T) {
	s := detectSources("asdf qwer zxcv hjkl")
	want := map[model.Source]bool{
		model.GitHub: true, model.HackerNews: true, model.Npm: true, model.PyPI: true, model.CratesIO: true,
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("selected %v, want %v", s, want)
	}
}

func TestHTTPClientBuilds(t *testing.T) {
	c := NewHTTPClient()
	if c == nil || c.Timeout != 10*time.Second {
		t.Fatalf("client = %+v", c)
	}
	if ua := UserAgent(); !strings.HasPrefix(ua, "silt/") || !strings.Contains(ua, "https://github.com/miku/silt") {
		t.Errorf("User-Agent = %q", ua)
	}
}

func TestSourcesForBuildsSelectedAdapters(t *testing.T) {
	ids := selectSources("a rust crate for parsing", nil, nil)
	if len(ids) == 0 {
		t.Fatal("no sources selected")
	}
	client := NewHTTPClient()
	for _, id := range ids {
		if a := (Searcher{}).newAdapter(id, client); a.ID() != id {
			t.Errorf("adapter for %v reports %v", id, a.ID())
		}
	}
}

func TestSelectSourcesUsesStableOrderAndFilters(t *testing.T) {
	// patent iterates a hash set; silt uses model.All order (a documented
	// deviation), so the same idea always gives the same order.
	idea := "a cli tool that kills a process on a port"
	want := []model.Source{model.CratesIO, model.GitHub, model.Npm, model.PyPI, model.HackerNews, model.Go, model.Homebrew}
	if got := selectSources(idea, nil, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("auto = %v, want %v", got, want)
	}
	if got := selectSources(idea, nil, []model.Source{model.PyPI, model.Go}); slices.Contains(got, model.PyPI) || slices.Contains(got, model.Go) {
		t.Errorf("exclude ignored: %v", got)
	}
	// An explicit include overrides detection, in All order whatever the
	// order given.
	got := selectSources(idea, []model.Source{model.Nixpkgs, model.AUR, model.Nixpkgs}, nil)
	if want := []model.Source{model.AUR, model.Nixpkgs}; !reflect.DeepEqual(got, want) {
		t.Errorf("include = %v, want %v", got, want)
	}
	if got := selectSources(idea, []model.Source{}, nil); len(got) != 0 {
		t.Errorf("an empty, non-nil include selects nothing, got %v", got)
	}
}

func TestEverySourceHasAnAdapterForTheLiveService(t *testing.T) {
	client := NewHTTPClient()
	for _, id := range model.All() {
		a := Searcher{}.newAdapter(id, client)
		if a.ID() != id {
			t.Errorf("adapter for %v reports %v", id, a.ID())
		}
	}
}

// ── Fan-out + dedup ──────────────────────────────────────────────────────

// failingSource always fails — it stands in for an unreachable registry.
type failingSource struct{}

func (failingSource) ID() model.Source { return model.PyPI }
func (failingSource) Search(context.Context, model.Query) ([]model.Match, error) {
	return nil, &silt.ParseError{Msg: "simulated failure"}
}

// countingSource counts its searches and always fails with err.
type countingSource struct {
	calls atomic.Int32
	err   error
}

func (*countingSource) ID() model.Source { return model.PyPI }
func (c *countingSource) Search(context.Context, model.Query) ([]model.Match, error) {
	c.calls.Add(1)
	return nil, c.err
}

// fastFanout keeps the tests quick; the shape of the timing is the same.
var fastFanout = fanout{timeout: 500 * time.Millisecond, retryDelay: 20 * time.Millisecond}

func sampleMatch(name, url string) model.Match {
	return model.Match{Name: name, Source: model.CratesIO, URL: url}
}

func TestDedupRemovesSameURLKeepingFirstAndOrder(t *testing.T) {
	out := Dedup([]model.Match{
		sampleMatch("a", "https://x/a"),
		sampleMatch("b", "https://x/b"),
		sampleMatch("a-dup", "https://x/a"), // same URL as the first
		sampleMatch("c", "https://x/c"),
	})
	var urls []string
	for _, m := range out {
		urls = append(urls, m.URL)
	}
	if !reflect.DeepEqual(urls, []string{"https://x/a", "https://x/b", "https://x/c"}) {
		t.Errorf("urls = %v", urls)
	}
	// The first occurrence is kept, not the later duplicate.
	if out[0].Name != "a" {
		t.Errorf("kept %q", out[0].Name)
	}
}

func TestDedupKeepsDistinctURLs(t *testing.T) {
	if n := len(Dedup([]model.Match{sampleMatch("a", "https://x/a"), sampleMatch("b", "https://x/b")})); n != 2 {
		t.Errorf("got %d matches, want 2", n)
	}
}

func TestDedupOfNothingIsEmptyNotNil(t *testing.T) {
	if out := Dedup(nil); out == nil || len(out) != 0 {
		t.Errorf("Dedup(nil) = %#v", out)
	}
}

// The real collision: a Homebrew formula whose homepage is a GitHub repo URL,
// alongside GitHub's own entry for that repo. Homebrew publishes no date and
// no star count; GitHub publishes both.
func TestDedupFillsGapsInTheKeptMatchFromALaterDuplicate(t *testing.T) {
	url := "https://github.com/skim-rs/skim"
	brew := model.Match{Name: "sk", Source: model.Homebrew, URL: url, Description: "Fuzzy Finder in rust!"}
	gh := model.Match{
		Name: "skim-rs/skim", Source: model.GitHub, URL: url, Description: "Fuzzy Finder in rust!",
		Popularity: ptr(uint64(5_600)), LastUpdated: ptr("2026-08-02T08:39:42Z"),
	}
	out := Dedup([]model.Match{brew, gh})
	if len(out) != 1 {
		t.Fatalf("got %d matches, want 1", len(out))
	}
	// Identity still belongs to the first arrival — enrichment fills gaps, it
	// does not hand the row to the other source.
	if out[0].Name != "sk" || out[0].Source != model.Homebrew {
		t.Errorf("kept %+v", out[0])
	}
	// ...but what only the duplicate knew is no longer lost.
	if deref(out[0].LastUpdated) != "2026-08-02T08:39:42Z" || deref(out[0].Popularity) != uint64(5_600) {
		t.Errorf("not enriched: %+v", out[0])
	}
}

func TestDedupNeverOverwritesWhatTheKeptMatchAlreadyKnows(t *testing.T) {
	first := sampleMatch("a", "https://x/a")
	first.LastUpdated, first.Popularity = ptr("2020-01-01T00:00:00Z"), ptr(uint64(1))
	later := sampleMatch("a-dup", "https://x/a")
	later.LastUpdated, later.Popularity = ptr("2026-01-01T00:00:00Z"), ptr(uint64(999))
	out := Dedup([]model.Match{first, later})
	if len(out) != 1 || deref(out[0].LastUpdated) != "2020-01-01T00:00:00Z" || deref(out[0].Popularity) != uint64(1) {
		t.Errorf("out = %+v", out)
	}
}

// Homepage-less entries fall through to a (name, source) key, and that path
// enriches too.
func TestDedupEnrichesAcrossTheNameSourceKeyAsWell(t *testing.T) {
	later := sampleMatch("lonely", "")
	later.LastUpdated = ptr("2026-03-01T00:00:00Z")
	out := Dedup([]model.Match{sampleMatch("lonely", ""), later})
	if len(out) != 1 || deref(out[0].LastUpdated) != "2026-03-01T00:00:00Z" {
		t.Errorf("out = %+v", out)
	}
}

func TestDedupDoesNotCollapseBlankURLsAcrossNames(t *testing.T) {
	out := Dedup([]model.Match{sampleMatch("a", " "), sampleMatch("b", ""), sampleMatch("a", "")})
	if len(out) != 2 {
		t.Errorf("out = %+v", out)
	}
}

func TestSearchSourcesSkipsFailingSources(t *testing.T) {
	srv := cratesIOServer(t, 200, twoCrateBody)
	out := SearchSources(context.Background(), []Adapter{failingSource{}, cratesIOFor(srv)}, query())
	// The failing source is skipped, never fatal: we still get the crates,
	// and the failed source is reported so reduced coverage is visible.
	if len(out.Matches) != 2 {
		t.Errorf("got %d matches, want 2", len(out.Matches))
	}
	for _, m := range out.Matches {
		if m.Source != model.CratesIO {
			t.Errorf("match from %v", m.Source)
		}
	}
	if !reflect.DeepEqual(out.Reached, []model.Source{model.CratesIO}) || !reflect.DeepEqual(out.Failed, []model.Source{model.PyPI}) {
		t.Errorf("reached %v, failed %v", out.Reached, out.Failed)
	}
}

func TestSearchSourcesDedupsAcrossSources(t *testing.T) {
	// Two identical crates.io sources -> overlapping URLs -> deduped to 2.
	srv := cratesIOServer(t, 200, twoCrateBody)
	out := SearchSources(context.Background(), []Adapter{cratesIOFor(srv), cratesIOFor(srv)}, query())
	if len(out.Matches) != 2 || len(out.Reached) != 2 {
		t.Errorf("matches %d, reached %v", len(out.Matches), out.Reached)
	}
}

func TestSearchSourcesEmptyWhenAllFail(t *testing.T) {
	out := fastFanout.search(context.Background(), []Adapter{failingSource{}, failingSource{}}, query())
	if len(out.Matches) != 0 || len(out.Reached) != 0 {
		t.Errorf("matches %v, reached %v", out.Matches, out.Reached)
	}
	if len(out.Failed) != 2 {
		t.Errorf("both failing sources must be reported, got %v", out.Failed)
	}
	if out.Matches == nil || out.Reached == nil {
		t.Error("empty results must be empty slices, not nil")
	}
}

func TestUnavailableSourceIsNotRetriedButTransientIs(t *testing.T) {
	// A persistently unavailable source (a walled search) must be attempted
	// exactly once — retrying only burns the delay on the same wall.
	unavailable := &countingSource{err: &silt.UnavailableError{Msg: "walled"}}
	// A transient failure (parse drift, network blip) must still be retried
	// once.
	transient := &countingSource{err: &silt.ParseError{Msg: "transient"}}
	out := fastFanout.search(context.Background(), []Adapter{unavailable, transient}, query())
	if n := unavailable.calls.Load(); n != 1 {
		t.Errorf("an Unavailable source must not be retried: %d calls", n)
	}
	if n := transient.calls.Load(); n != 2 {
		t.Errorf("a transient failure must be retried exactly once: %d calls", n)
	}
	// Both still land in Failed — best-effort, never fatal.
	if len(out.Failed) != 2 {
		t.Errorf("failed = %v", out.Failed)
	}
}

// failNTimes serves 500 for the first n requests, then h.
func failNTimes(n int32, h http.Handler) http.HandlerFunc {
	var served atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		if served.Add(1) <= n {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		h.ServeHTTP(w, r)
	}
}

func TestTransientFailureRecoversOnRetry(t *testing.T) {
	srv := newMockServer(t, onPath("/api/v1/crates", failNTimes(1, respond(200, twoCrateBody))))
	out := fastFanout.search(context.Background(), []Adapter{cratesIOFor(srv)}, query())
	if len(out.Matches) != 2 || !reflect.DeepEqual(out.Reached, []model.Source{model.CratesIO}) || len(out.Failed) != 0 {
		t.Errorf("outcome = %+v", out)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("got %d requests, want 2", n)
	}
}

// hang never answers until the client gives up.
func hang(w http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(20 * time.Second):
	}
}

func TestHangingSourceTimesOut(t *testing.T) {
	srv := newMockServer(t, http.HandlerFunc(hang))
	start := time.Now()
	out := fastFanout.search(context.Background(), []Adapter{cratesIOFor(srv)}, query())
	if len(out.Matches) != 0 || len(out.Reached) != 0 || !reflect.DeepEqual(out.Failed, []model.Source{model.CratesIO}) {
		t.Errorf("outcome = %+v", out)
	}
	if elapsed := time.Since(start); elapsed > fastFanout.timeout+time.Second {
		t.Errorf("fan-out took %v, deadline was %v", elapsed, fastFanout.timeout)
	}
}

// blindSource ignores its context and blocks until released.
type blindSource struct{ release chan struct{} }

func (blindSource) ID() model.Source { return model.Go }
func (b blindSource) Search(context.Context, model.Query) ([]model.Match, error) {
	<-b.release
	return []model.Match{sampleMatch("late", "https://x/late")}, nil
}

func TestASourceThatIgnoresItsContextStillTimesOut(t *testing.T) {
	b := blindSource{release: make(chan struct{})}
	defer close(b.release)
	out := fastFanout.search(context.Background(), []Adapter{b}, query())
	if !reflect.DeepEqual(out.Failed, []model.Source{model.Go}) || len(out.Matches) != 0 {
		t.Errorf("outcome = %+v", out)
	}
	r := fastFanout.searchOne(context.Background(), b, query())
	if r.err == nil || r.err.Error() != "failed to parse response: Go timed out" {
		t.Errorf("err = %v", r.err)
	}
}

// walledSource fronts a registry that answers 403 to every request, which
// patent's PyPI adapter reports as Unavailable.
type walledSource struct{ srv *mockServer }

func (walledSource) ID() model.Source { return model.PyPI }
func (w walledSource) Search(ctx context.Context, _ model.Query) ([]model.Match, error) {
	req, err := newRequest(ctx, http.MethodGet, w.srv.URL+"/search/", nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := send(NewHTTPClient(), req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, &silt.UnavailableError{Msg: "PyPI search is bot-walled"}
	}
	return nil, checkStatus(resp)
}

// TestFanOutReachedFailedSplitWithinDeadlines is M2's acceptance test: one
// slow source, one that recovers from a 500 on its retry, and one behind a
// wall, all at once.
func TestFanOutReachedFailedSplitWithinDeadlines(t *testing.T) {
	slow := newMockServer(t, http.HandlerFunc(hang))
	flaky := newMockServer(t, onPath("/search/repositories", failNTimes(1, respond(200, gitHubBody))))
	walled := newMockServer(t, respond(403, "<html>challenge</html>"))

	f := fanout{timeout: time.Second, retryDelay: 100 * time.Millisecond}
	start := time.Now()
	out := f.search(context.Background(), []Adapter{cratesIOFor(slow), gitHubFor(flaky), walledSource{walled}}, query())
	elapsed := time.Since(start)

	if want := []model.Source{model.GitHub}; !reflect.DeepEqual(out.Reached, want) {
		t.Errorf("reached = %v, want %v", out.Reached, want)
	}
	if want := []model.Source{model.CratesIO, model.PyPI}; !reflect.DeepEqual(out.Failed, want) {
		t.Errorf("failed = %v, want %v", out.Failed, want)
	}
	if len(out.Matches) != 2 {
		t.Errorf("got %d matches, want GitHub's 2", len(out.Matches))
	}
	// Retried once, after a 500.
	if n := len(flaky.Requests()); n != 2 {
		t.Errorf("flaky source: %d requests, want 2", n)
	}
	// Never retried: a wall stays a wall.
	if n := len(walled.Requests()); n != 1 {
		t.Errorf("walled source: %d requests, want 1", n)
	}
	// The slow source is cut off at the deadline, and nothing waits past it.
	if elapsed < f.timeout || elapsed > f.timeout+500*time.Millisecond {
		t.Errorf("fan-out took %v, want about %v", elapsed, f.timeout)
	}
}

func TestFanOutRunsSourcesConcurrently(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	slowOK := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		respond(200, `{"crates": []}`)(w, r)
	}
	srv := newMockServer(t, http.HandlerFunc(slowOK))
	out := fastFanout.search(context.Background(), []Adapter{cratesIOFor(srv), cratesIOFor(srv), cratesIOFor(srv)}, model.Query{Idea: "x", Keywords: []string{"x"}})
	if len(out.Reached) != 3 || peak != 3 {
		t.Errorf("reached %v, peak concurrency %d", out.Reached, peak)
	}
}
