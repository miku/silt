// Package sources is the source registry: one adapter per ecosystem, fanned
// out concurrently.
//
// Sources are selected based on the query — a Rust query searches crates.io,
// a Python query searches PyPI, and so on. GitHub and Hacker News are always
// included. When no language is detected, the three largest registries (npm,
// PyPI, crates.io) are used as a broad fallback.
//
// # Which sources report a last-updated date
//
// [model.Match].LastUpdated is populated only where the search response
// already carries a date. Fetching one per result would mean an extra request
// per match, and with the per-source timeout bounding the whole source that
// is not a trade worth making. The split below was established by probing
// each live API, so the gaps are deliberate rather than unfinished:
//
//	Populated              Field
//	crates.io              updated_at
//	npm                    package.date
//	GitHub                 pushed_at (last push, not metadata edits)
//	Hex                    updated_at
//	Maven                  timestamp (epoch millis)
//	AUR                    LastModified (epoch secs)
//	Artifact Hub           ts (epoch secs)
//	Go                     scraped from the rendered "published on" date
//	JetBrains Marketplace  cdate (epoch millis, last-updated not creation)
//	VS Code Marketplace    lastUpdated (needs flags: 914; a narrower flag set silently drops it)
//
// Always nil because the date exists only behind a per-package request, which
// the timeout does not buy: RubyGems, Docker Hub, NuGet, Packagist, Homebrew
// and Nixpkgs. PyPI is bot-walled and returns nothing to parse.
//
// Two more sources publish a date that is not a last-updated date, and stay
// nil on purpose. The field means "when the artifact itself last changed":
// Hacker News created_at is when a thread was posted, and Hackage sends a
// last-modified header for the .cabal file, which trustee bound-fixes bump
// without a release. Either one would render staleness about something that
// was never being maintained in the first place.
//
// One path deliberately not taken: Docker Hub's v3 catalog endpoint
// (/api/search/v3/catalog/search) does carry updated_at, but reaching it
// means swapping search endpoints, not reading another field.
//
// The table describes what each source publishes, not what every rendered
// row ends up carrying. A match attributed to an undated source can still show
// a date if another source returned the same URL: [Dedup] merges duplicates
// and fills the kept match's gaps. A Homebrew formula whose homepage is a
// GitHub repo inherits that repo's pushed_at — a date about the same
// artifact, from the source that does publish one.
//
// [model.Match]: https://pkg.go.dev/github.com/miku/silt/model#Match
package sources

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/model"
)

// Adapter is one searchable ecosystem: a registry, a forge, a community
// index.
type Adapter interface {
	// ID is the stable identifier, used in the transparency line.
	ID() model.Source
	// Search searches this source for prior art matching q.
	Search(ctx context.Context, q model.Query) ([]model.Match, error)
}

// narrowed picks the n longest keywords, preserving their original order (not
// sorted by length): the longest terms carry the most meaning, but reordering
// them changes what a relevance-ranked registry returns. Ties keep their
// original order too, as Rust's stable sort does.
func narrowed(keywords []string, n int) string {
	idx := make([]int, len(keywords))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		return cmp.Compare(len(keywords[b]), len(keywords[a]))
	})
	idx = idx[:min(n, len(idx))]
	slices.Sort(idx)
	terms := make([]string, len(idx))
	for i, k := range idx {
		terms[i] = keywords[k]
	}
	return strings.Join(terms, " ")
}

// narrowingCandidates returns the query strings to try, in order, against a
// source that ANDs its search terms.
//
// Such a source returns nothing for a realistic multi-keyword idea even where
// matching packages exist — measured live, a 7-term idea returns zero from
// half the registries while 2-3 of its longest terms return hundreds. Callers
// try each candidate until one comes back non-empty.
//
// The list runs from the full keyword set down to minTerms of the longest
// keywords; duplicates (a narrowing that repeats a string already tried) are
// dropped, and a query with no keywords at all falls back to the raw idea
// rather than sending an empty search param. minTerms is per-source and
// measured, not guessed: most sources recover at 2, while Maven, NuGet and
// Artifact Hub index strictly enough to need a single keyword.
func narrowingCandidates(q model.Query, minTerms int) []string {
	steps := []int{len(q.Keywords)}
	for n := 3; n >= minTerms; n-- {
		steps = append(steps, n)
	}
	var candidates []string
	for _, n := range steps {
		if s := narrowed(q.Keywords, n); s != "" && !slices.Contains(candidates, s) {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		candidates = append(candidates, q.Idea)
	}
	return candidates
}

func isASCIIAlphanumeric(b byte) bool {
	return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || '0' <= b && b <= '9'
}

// ideaContains reports whether any of terms occurs in idea as a whole word,
// ignoring case. Word boundaries are ASCII: any byte that is not an ASCII
// letter or digit separates words, so "c#" and ".net" work as terms.
func ideaContains(idea string, terms []string) bool {
	lower := textutil.ToLower(idea)
	for _, t := range terms {
		if t == "" {
			continue
		}
		// Check every occurrence, not just the first: a short keyword like
		// "go" may first appear inside a larger word ("django") and then
		// again as a standalone word — only the standalone one should count.
		for start := 0; ; {
			i := strings.Index(lower[start:], t)
			if i < 0 {
				break
			}
			pos := start + i
			end := pos + len(t)
			before := pos == 0 || !isASCIIAlphanumeric(lower[pos-1])
			after := end >= len(lower) || !isASCIIAlphanumeric(lower[end])
			if before && after {
				return true
			}
			start = end
		}
	}
	return false
}

// selectionRules map words in an idea to the sources worth searching for it.
// Every rule whose terms match adds its sources; see detectSources.
var selectionRules = []struct {
	terms   []string
	sources []model.Source
}{
	// ── Explicit language / ecosystem mentions ──────────────────────────
	{[]string{"rust", "crate", "cargo"}, []model.Source{model.CratesIO}},
	{[]string{"brew", "homebrew", "macos", "cask"}, []model.Source{model.Homebrew}},
	{[]string{"npm", "node", "javascript", "typescript", "deno", "bun"}, []model.Source{model.Npm}},
	{[]string{"python", "pip", "django", "flask", "pytorch", "pandas"}, []model.Source{model.PyPI}},
	{[]string{"go", "golang", "goroutine"}, []model.Source{model.Go}},
	{[]string{"java", "kotlin", "spring", "maven", "gradle", "scala"}, []model.Source{model.Maven}},
	{[]string{"ruby", "rails", "sinatra", "gem"}, []model.Source{model.RubyGems}},
	{[]string{"c#", ".net", "csharp", "dotnet", "nuget", "blazor", "unity"}, []model.Source{model.NuGet}},
	{[]string{"php", "composer", "laravel", "symfony"}, []model.Source{model.Packagist}},
	{[]string{"elixir", "erlang", "phoenix", "hex", "mix"}, []model.Source{model.Hex}},
	{
		[]string{"helm", "kubernetes", "k8s", "cncf", "cloud-native", "operator", "kubectl", "crd"},
		[]model.Source{model.ArtifactHub},
	},
	{[]string{"arch", "aur", "pacman", "archlinux"}, []model.Source{model.AUR}},
	{[]string{"haskell", "cabal", "hackage", "ghc", "stack"}, []model.Source{model.Hackage}},
	{[]string{"nix", "nixos", "nixpkgs", "flake"}, []model.Source{model.Nixpkgs}},

	// ── Domain sweeps ───────────────────────────────────────────────────
	{
		[]string{
			"ai", "llm", "machine learning", "deep learning", "neural", "model training",
			"inference", "embedding", "nlp", "computer vision", "data science", "data pipeline",
		},
		[]model.Source{model.PyPI, model.Npm},
	},
	// CLI tools span every ecosystem, so a generic "cli" mention casts the net
	// across the dominant registries rather than just Rust/Go — otherwise the
	// flagship "kill the process on a port" demo would never search npm,
	// where fkill-cli / kill-port actually live.
	{
		[]string{"cli", "command line", "terminal tool", "shell"},
		[]model.Source{model.CratesIO, model.Go, model.Npm, model.PyPI, model.Homebrew},
	},
	{
		[]string{
			"frontend", "react", "vue", "angular", "svelte", "browser", "css",
			"ui component", "web component", "spa",
		},
		[]model.Source{model.Npm},
	},
	{
		[]string{"api", "backend", "rest", "graphql", "microservice", "web server"},
		[]model.Source{model.Npm, model.PyPI, model.Go},
	},
	{
		[]string{"mobile", "ios", "android", "react native", "flutter", "swift", "swiftui"},
		[]model.Source{model.Npm, model.Maven},
	},
	{
		[]string{"game", "graphics", "rendering", "opengl", "vulkan", "bevy", "godot"},
		[]model.Source{model.CratesIO, model.NuGet},
	},
	{[]string{"embedded", "firmware", "microcontroller", "rtos"}, []model.Source{model.CratesIO}},
	{
		[]string{"docker", "container", "kubernetes", "k8s", "helm", "deploy", "infrastructure"},
		[]model.Source{model.DockerHub, model.Go},
	},
	{
		[]string{"vscode", "extension", "plugin", "ide", "editor"},
		[]model.Source{model.VSCodeMarketplace, model.Npm, model.JetBrains},
	},
	{
		[]string{
			"jetbrains", "intellij", "pycharm", "webstorm", "goland", "rider", "clion",
			"datagrip", "phpstorm", "rubymine", "android studio",
		},
		[]model.Source{model.JetBrains},
	},
}

// alwaysOn are searched for every idea: GitHub is the cross-language home of
// source code, and Hacker News is the cross-language home of the "Show HN"
// launch / discussion that often predates a registry release.
var alwaysOn = []model.Source{model.GitHub, model.HackerNews}

// fallback is added when no rule matched: the always-on sources aren't
// enough on their own, so sweep the three biggest registries.
var fallback = []model.Source{model.Npm, model.PyPI, model.CratesIO}

// detectSources picks the sources to search for idea.
func detectSources(idea string) map[model.Source]bool {
	selected := make(map[model.Source]bool)
	for _, s := range alwaysOn {
		selected[s] = true
	}
	for _, r := range selectionRules {
		if ideaContains(idea, r.terms) {
			for _, s := range r.sources {
				selected[s] = true
			}
		}
	}
	if len(selected) <= len(alwaysOn) {
		for _, s := range fallback {
			selected[s] = true
		}
	}
	return selected
}

// newAdapter builds the adapter for id.
func newAdapter(id model.Source, client *http.Client) Adapter {
	switch id {
	case model.CratesIO:
		return NewCratesIO(client)
	case model.GitHub:
		return NewGitHub(client)
	case model.HackerNews:
		return NewHackerNews(client)
	default:
		return unported(id)
	}
}

// unported stands in for a source whose adapter has not been ported from
// patent yet. It fails as unavailable, so the source is reported as not
// reached — never silently dropped, which would overstate coverage.
type unported model.Source

func (u unported) ID() model.Source { return model.Source(u) }

func (u unported) Search(context.Context, model.Query) ([]model.Match, error) {
	return nil, &silt.UnavailableError{Msg: fmt.Sprintf("%s is not supported by silt yet", model.Source(u))}
}

// Outcome is the result of a fan-out: deduped matches, the sources that
// responded, and the selected sources that failed (so that reduced coverage
// can be surfaced).
type Outcome struct {
	Matches []model.Match
	Reached []model.Source
	Failed  []model.Source
}

// SearchAll fans out to the sources detected for q. Individual source
// failures are not errors; they are reported in Outcome.Failed.
func SearchAll(ctx context.Context, q model.Query) Outcome {
	return SearchFiltered(ctx, q, nil, nil)
}

// SearchFiltered is like [SearchAll], but with explicit control over which
// sources to use. A nil include runs the auto-detection as usual; otherwise
// exactly the sources in include are searched. Sources in exclude are removed
// from the final set either way.
//
// Sources are searched and reported in [model.All] order. (patent iterates a
// hash set, so its order — and which duplicate wins in [Dedup] — changes from
// run to run.)
func SearchFiltered(ctx context.Context, q model.Query, include, exclude []model.Source) Outcome {
	client := NewHTTPClient()
	var adapters []Adapter
	for _, id := range selectSources(q.Idea, include, exclude) {
		adapters = append(adapters, newAdapter(id, client))
	}
	return SearchSources(ctx, adapters, q)
}

// selectSources returns the sources SearchFiltered searches, in model.All
// order.
func selectSources(idea string, include, exclude []model.Source) []model.Source {
	var selected map[model.Source]bool
	if include == nil {
		selected = detectSources(idea)
	} else {
		selected = make(map[model.Source]bool)
		for _, s := range include {
			selected[s] = true
		}
	}
	for _, s := range exclude {
		delete(selected, s)
	}
	var ids []model.Source
	for _, id := range model.All() {
		if selected[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

// fanout holds the timing of a fan-out.
type fanout struct {
	// timeout is the per-source wall-clock limit. The default is generous
	// enough for the HTTP timeout (10 s) plus the retry delay plus a second
	// attempt, but prevents a single slow source from holding up the entire
	// fan-out indefinitely.
	timeout time.Duration
	// retryDelay is the pause before the one retry of a transient failure.
	retryDelay time.Duration
}

var defaultFanout = fanout{timeout: 15 * time.Second, retryDelay: 800 * time.Millisecond}

// SearchSources runs q against adapters concurrently and dedups the combined
// results. A failing source is skipped, never fatal: it is reported in
// Outcome.Failed, in adapter order, as reached sources are in
// Outcome.Reached. Nothing is printed, since output would draw over the TUI.
func SearchSources(ctx context.Context, adapters []Adapter, q model.Query) Outcome {
	return defaultFanout.search(ctx, adapters, q)
}

type result struct {
	matches []model.Match
	err     error
}

func (f fanout) search(ctx context.Context, adapters []Adapter, q model.Query) Outcome {
	results := make([]result, len(adapters))
	var wg sync.WaitGroup
	for i, a := range adapters {
		wg.Go(func() { results[i] = f.searchOne(ctx, a, q) })
	}
	wg.Wait()
	out := Outcome{Reached: []model.Source{}, Failed: []model.Source{}}
	var all []model.Match
	for i, r := range results {
		if r.err != nil {
			out.Failed = append(out.Failed, adapters[i].ID())
			continue
		}
		out.Reached = append(out.Reached, adapters[i].ID())
		all = append(all, r.matches...)
	}
	out.Matches = Dedup(all)
	return out
}

// searchOne searches a single source within the fan-out's timeout, retrying a
// retryable failure once.
func (f fanout) searchOne(ctx context.Context, a Adapter, q model.Query) result {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	ch := make(chan result, 1) // buffered: an adapter that ignores ctx must not leak
	go func() {
		matches, err := a.Search(ctx, q)
		if err != nil && silt.IsRetryable(err) {
			select {
			case <-time.After(f.retryDelay):
				matches, err = a.Search(ctx, q)
			case <-ctx.Done():
			}
		}
		ch <- result{matches, err}
	}()
	select {
	case r := <-ch:
		return r
	case <-ctx.Done():
		select {
		case r := <-ch: // finished right at the deadline
			return r
		default:
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return result{err: &silt.ParseError{Msg: fmt.Sprintf("%s timed out", a.ID())}}
		}
		return result{err: ctx.Err()}
	}
}

// Dedup removes duplicate matches by URL, keeping the first occurrence and
// preserving order, but filling that first occurrence's gaps from the
// duplicates behind it. URL is a match's canonical identity across sources.
//
// Empty or whitespace URLs are never used as a dedup key — they would collapse
// all homepage-less entries (e.g. Homebrew formulae with no homepage) into one
// slot, silently dropping every subsequent one. Those fall through to a
// (name, source) key instead.
//
// The enrichment matters because sources know different things about the same
// artifact: GitHub reports a repo's pushed_at and star count, while the
// Homebrew formula whose homepage is that repo reports neither (35% of
// formulae have a homepage byte-identical to a GitHub repo URL, so this is a
// routine collision, not a corner case).
func Dedup(matches []model.Match) []model.Match {
	type nameSource struct {
		name   string
		source model.Source
	}
	var (
		byURL        = make(map[string]int)
		byNameSource = make(map[nameSource]int)
		kept         = make([]model.Match, 0, len(matches))
	)
	for _, m := range matches {
		keyedByURL := strings.TrimSpace(m.URL) != ""
		var (
			i  int
			ok bool
		)
		if keyedByURL {
			i, ok = byURL[m.URL]
		} else {
			i, ok = byNameSource[nameSource{m.Name, m.Source}]
		}
		if ok {
			enrich(&kept[i], m)
			continue
		}
		if keyedByURL {
			byURL[m.URL] = len(kept)
		} else {
			byNameSource[nameSource{m.Name, m.Source}] = len(kept)
		}
		kept = append(kept, m)
	}
	return kept
}

// enrich fills in what the kept match doesn't know from a later duplicate of
// the same artifact.
//
// It only ever fills gaps. A value the kept copy already carries is never
// overwritten, so which source "owns" a shared URL — its name, description and
// the rest — stays exactly what it was before: the first one to arrive.
func enrich(kept *model.Match, dup model.Match) {
	if kept.LastUpdated == nil {
		kept.LastUpdated = dup.LastUpdated
	}
	if kept.Popularity == nil {
		kept.Popularity = dup.Popularity
	}
}
