package sources

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

// ── JetBrains Marketplace ─────────────────────────────────────────────────

const jetbrainsBody = `{"plugins": [{
  "id": 164, "xmlId": "IdeaVIM", "link": "/plugin/164-ideavim", "name": "IdeaVim",
  "preview": "Bring the power of Vim to your JetBrains IDE.", "downloads": 21803835,
  "pricingModel": "FREE", "cdate": 1785747916000, "rating": 4.44,
  "tags": ["Editor", "Keymap"], "vendor": {"name": "JetBrains s.r.o.", "isVerified": true}
}], "total": 1, "correctedQuery": ""}`

func jetbrainsFor(srv *mockServer) *JetBrains {
	return &JetBrains{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func jetbrainsServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/api/searchPlugins", respond(status, body)))
}

func jetbrainsPlugins(t *testing.T, plugins string) []model.Match {
	return mustSearch(t, jetbrainsFor(jetbrainsServer(t, 200, `{"plugins": `+plugins+`}`)), query())
}

func TestJetBrainsIDIsJetBrains(t *testing.T) {
	if got := NewJetBrains(nil).ID(); got != model.JetBrains {
		t.Fatalf("ID() = %v", got)
	}
}

func TestJetBrainsMapsPluginsIntoMatches(t *testing.T) {
	srv := jetbrainsServer(t, 200, jetbrainsBody)
	m := mustSearch(t, jetbrainsFor(srv), query())
	// The URL is built against the mock server, proving the relative link
	// join; cdate is epoch millis.
	if len(m) != 1 || m[0].Name != "IdeaVim" || m[0].Source != model.JetBrains || m[0].URL != srv.URL+"/plugin/164-ideavim" ||
		m[0].Description != "Bring the power of Vim to your JetBrains IDE." || deref(m[0].Popularity) != uint64(21_803_835) ||
		deref(m[0].LastUpdated) != "2026-08-03T09:05:16Z" {
		t.Errorf("matches = %+v", m)
	}
	if params := srv.Requests()[0].URL.Query(); params.Get("search") != "async runtime" || params.Get("max") != "20" {
		t.Errorf("params = %v", params)
	}
}

func TestJetBrainsDescriptionFallsBackToNameWhenPreviewMissing(t *testing.T) {
	m := jetbrainsPlugins(t, `[{"name": "NoPreview", "link": "/plugin/1-no-preview"}, {"name": "BlankPreview", "link": "/plugin/5-blank-preview", "preview": "   "}]`)
	// A preview that is only whitespace falls back like an absent one.
	if m[0].Description != "NoPreview" || m[1].Description != "BlankPreview" {
		t.Errorf("matches = %+v", m)
	}
}

func TestJetBrainsTruncatesLongDescriptions(t *testing.T) {
	m := jetbrainsPlugins(t, `[{"name": "LongDescPlugin", "link": "/plugin/2-long-desc", "preview": "`+strings.Repeat("a", 200)+`"}]`)
	if d := m[0].Description; utf8.RuneCountInString(d) != 121 || !strings.HasSuffix(d, "…") {
		t.Errorf("description = %q", d)
	}
}

func TestJetBrainsTruncatesLongMultibyteDescriptions(t *testing.T) {
	// Truncating at a byte offset would split a character.
	m := jetbrainsPlugins(t, `[{"name": "MultibyteDescPlugin", "link": "/plugin/7-multibyte-desc", "preview": "`+strings.Repeat("é", 200)+`"}]`)
	if d := m[0].Description; utf8.RuneCountInString(d) != 121 || !strings.HasSuffix(d, "…") || !strings.HasPrefix(d, "é") || !utf8.ValidString(d) {
		t.Errorf("description = %q", d)
	}
}

func TestJetBrainsSkipsPluginWithMissingName(t *testing.T) {
	if m := jetbrainsPlugins(t, `[{"link": "/plugin/3-no-name", "preview": "no name here"}, {"name": "", "link": "/plugin/4-empty-name"}]`); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestJetBrainsSkipsPluginWithMissingLink(t *testing.T) {
	if m := jetbrainsPlugins(t, `[{"name": "NoLink"}, {"name": "EmptyLink", "link": ""}, {"name": "BareHostLink", "link": "plugin/5-bare"}]`); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestJetBrainsWrongTypeCdateDegradesToNoneNotError(t *testing.T) {
	m := jetbrainsPlugins(t, `[{"name": "BadDate", "link": "/plugin/6-bad-date", "cdate": "not-a-number"}]`)
	if len(m) != 1 || m[0].LastUpdated != nil {
		t.Errorf("matches = %+v", m)
	}
}

func TestJetBrainsEmptyResultsIsOK(t *testing.T) {
	if m := jetbrainsPlugins(t, `[]`); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestJetBrainsServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, jetbrainsFor(jetbrainsServer(t, 500, "")), query())
}

func TestJetBrainsMalformedBodyIsPropagated(t *testing.T) {
	wantSearchError(t, jetbrainsFor(jetbrainsServer(t, 200, "<html>not json</html>")), query())
}

func TestJetBrainsMissingPluginsKeyIsPropagated(t *testing.T) {
	// Valid JSON without the plugins key must be an error, not a silent
	// empty result.
	wantSearchError(t, jetbrainsFor(jetbrainsServer(t, 200, `{"total": 0, "correctedQuery": ""}`)), query())
}

func jetbrainsNarrowing(t *testing.T, empty []string, found string) *mockServer {
	return narrowingServer(t, "/api/searchPlugins", "search", empty, `{"plugins": []}`, found, jetbrainsBody)
}

func TestJetBrainsNarrowsToThreeLongestKeywordsWhenFullQueryIsEmpty(t *testing.T) {
	srv := jetbrainsNarrowing(t, narrowingSteps[:1], "spell syntax mistakes")
	if m := mustSearch(t, jetbrainsFor(srv), narrowingQuery()); len(m) != 1 || m[0].Name != "IdeaVim" || len(srv.Requests()) != 2 {
		t.Errorf("matches = %+v after %q", m, requested(srv, "search"))
	}
}

func TestJetBrainsFallsBackToTwoLongestKeywords(t *testing.T) {
	srv := jetbrainsNarrowing(t, narrowingSteps[:2], "syntax mistakes")
	if m := mustSearch(t, jetbrainsFor(srv), narrowingQuery()); len(m) != 1 || m[0].Name != "IdeaVim" || len(srv.Requests()) != 3 {
		t.Errorf("matches = %+v after %q", m, requested(srv, "search"))
	}
}

func TestJetBrainsAllNarrowingAttemptsEmptyIsOKNotErr(t *testing.T) {
	srv := jetbrainsNarrowing(t, narrowingSteps[:3], "unused")
	if m := mustSearch(t, jetbrainsFor(srv), narrowingQuery()); len(m) != 0 || len(srv.Requests()) != 3 {
		t.Errorf("matches = %+v after %q", m, requested(srv, "search"))
	}
}

func TestJetBrainsShortKeywordSetIssuesOnlyOneRequest(t *testing.T) {
	// With ≤3 keywords every narrowing is textually the full query, so it
	// must be sent once, not three times.
	srv := jetbrainsNarrowing(t, []string{"async runtime"}, "unused")
	if m := mustSearch(t, jetbrainsFor(srv), query()); len(m) != 0 || len(srv.Requests()) != 1 {
		t.Errorf("matches = %+v after %q", m, requested(srv, "search"))
	}
}

// ── Homebrew ──────────────────────────────────────────────────────────────

// brewServer serves the two catalogs and counts the requests for each.
func brewServer(t *testing.T, formulae, casks string) *mockServer {
	return newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/formula.json":
			respond(200, formulae)(w, r)
		case "/api/cask.json":
			respond(200, casks)(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
}

func brewFor(srv *mockServer) *Homebrew { return &Homebrew{Client: NewHTTPClient(), BaseURL: srv.URL} }

func TestHomebrewHappyPath(t *testing.T) {
	srv := brewServer(t,
		`[{"name": "ripgrep", "desc": "Search tool like grep and The Silver Searcher", "homepage": "https://github.com/BurntSushi/ripgrep"}]`,
		`[{"token": "google-chrome", "desc": "Web browser", "homepage": "https://www.google.com/chrome/"}]`)
	m := mustSearch(t, brewFor(srv), model.Query{Idea: "A fast search tool", Keywords: []string{"ripgrep"}})
	if len(m) != 1 || m[0].Name != "ripgrep" || m[0].Source != model.Homebrew || m[0].URL != "https://github.com/BurntSushi/ripgrep" {
		t.Errorf("matches = %+v", m)
	}
	if got := srv.Requests()[0].Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
}

func TestHomebrewEmptyResults(t *testing.T) {
	m := mustSearch(t, brewFor(brewServer(t, `[]`, `[]`)), model.Query{Idea: "Something completely obscure", Keywords: []string{"doesnotexist123"}})
	if len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestHomebrewServerError(t *testing.T) {
	srv := newMockServer(t, respond(500, ""))
	wantSearchError(t, brewFor(srv), model.Query{Idea: "Testing server crash", Keywords: []string{"test"}})
	// The first failed request short-circuits: the casks are never fetched.
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

// Homebrew ANDs locally against a one-line description, so the narrowing runs
// over the cached catalog rather than over requests.
func TestHomebrewNarrowsAgainstTheCachedCatalog(t *testing.T) {
	// Carries spell, syntax and mistakes but neither "ide" nor "code", so only
	// the narrowed keyword set can match it.
	srv := brewServer(t, `[{"name": "proselint", "desc": "Finds syntax mistakes and spell errors in prose", "homepage": "https://proselint.com"}]`, `[]`)
	m := mustSearch(t, brewFor(srv), narrowingQuery())
	if len(m) != 1 || m[0].Name != "proselint" || len(srv.Requests()) != 2 {
		t.Errorf("matches = %+v after %d requests", m, len(srv.Requests()))
	}
}

func TestHomebrewCachesTheCatalogAndMatchesCasksAndCase(t *testing.T) {
	srv := brewServer(t, `[{"name": "fd", "desc": null}]`, `[{"token": "Firefox", "desc": "Web BROWSER"}]`)
	a := brewFor(srv)
	m := mustSearch(t, a, model.Query{Idea: "x", Keywords: []string{"browser"}})
	// No homepage: the formula page on the configured host.
	if len(m) != 1 || m[0].Name != "Firefox" || m[0].URL != srv.URL+"/formula/Firefox" || m[0].Description != "Web BROWSER" {
		t.Errorf("matches = %+v", m)
	}
	mustSearch(t, a, model.Query{Idea: "y", Keywords: []string{"fd"}})
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("%d requests for two searches, want the 2 catalog fetches once", n)
	}
}

func TestHomebrewKeepsAtMost20(t *testing.T) {
	var b strings.Builder
	b.WriteString(`[`)
	for i := range 25 {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, `{"name": "tool%d", "desc": "a shell tool"}`, i)
	}
	b.WriteString(`]`)
	m := mustSearch(t, brewFor(brewServer(t, b.String(), `[]`)), model.Query{Idea: "x", Keywords: []string{"shell"}})
	if len(m) != 20 || m[0].Name != "tool0" {
		t.Errorf("got %d matches", len(m))
	}
}

func TestHomebrewRetriesAFailedCatalogFetch(t *testing.T) {
	var ok atomic.Bool
	catalogs := brewServer(t, `[{"name": "jq", "desc": "JSON processor"}]`, `[{"token": "jqview", "desc": "JSON viewer"}]`)
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		catalogs.Config.Handler.ServeHTTP(w, r)
	}))
	a := brewFor(srv)
	q := model.Query{Idea: "x", Keywords: []string{"json"}}
	wantSearchError(t, a, q)
	ok.Store(true)
	if m := mustSearch(t, a, q); len(m) != 2 {
		t.Errorf("matches = %+v; the failed fetch must not have been cached", m)
	}
}

// brewCatalogServer serves catalogs with fields the search doesn't read, as
// the real ones have, and a formula without a homepage.
func brewCatalogServer(t *testing.T) *mockServer {
	return brewServer(t,
		`[{"name": "jq", "desc": "Lightweight and flexible command-line JSON processor", "homepage": "https://jqlang.org/",
		   "versions": {"stable": "1.8.1"}, "dependencies": ["oniguruma"]},
		  {"name": "jless", "desc": "Command-line pager for JSON data"}]`,
		`[{"token": "jsonviewer", "desc": "JSON viewer", "homepage": "https://example.com/jv", "url": "https://example.com/jv.dmg"}]`)
}

// runBrew searches like a new silt process: with its own in-memory catalog, and
// the catalogs on disk.
func runBrew(t *testing.T, srv *mockServer, c *CatalogCache) []model.Match {
	t.Helper()
	a := brewFor(srv)
	a.Catalogs = c
	return mustSearch(t, a, model.Query{Idea: "x", Keywords: []string{"json"}})
}

func testCatalogs(t *testing.T) *CatalogCache {
	return &CatalogCache{Dir: filepath.Join(t.TempDir(), "catalogs"), TTL: 24 * time.Hour}
}

func TestHomebrewKeepsItsCatalogOnDisk(t *testing.T) {
	srv := brewCatalogServer(t)
	c := testCatalogs(t)
	first := runBrew(t, srv, c)
	if len(first) != 3 || len(srv.Requests()) != 2 {
		t.Fatalf("first run: %d matches, %d requests", len(first), len(srv.Requests()))
	}
	second := runBrew(t, srv, c)
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("the second run sent %d more requests, want none", n-2)
	}
	// The same matches, in the same order, with the same fallback URL.
	if !reflect.DeepEqual(second, first) || second[1].URL != srv.URL+"/formula/jless" {
		t.Errorf("from disk:\n got %+v\nwant %+v", second, first)
	}

	b, err := os.ReadFile(filepath.Join(c.Dir, "homebrew.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"versions", "dependencies", "oniguruma", "jv.dmg"} {
		if strings.Contains(string(b), field) {
			t.Errorf("the catalog on disk keeps %q, which the search doesn't read", field)
		}
	}
	if fi, err := os.Stat(c.Dir); err != nil {
		t.Error(err)
	} else if fi.Mode().Perm() != 0o700 && runtime.GOOS != "windows" {
		t.Errorf("catalog directory mode %v, want 0700", fi.Mode())
	}
}

func TestHomebrewDownloadsAgainWhenTheCatalogOnDiskIsNoGood(t *testing.T) {
	path := func(c *CatalogCache) string { return filepath.Join(c.Dir, "homebrew.json") }
	for name, spoil := range map[string]func(t *testing.T, c *CatalogCache){
		"expired": func(t *testing.T, c *CatalogCache) {
			old := time.Now().Add(-c.TTL - time.Minute)
			if err := os.Chtimes(path(c), old, old); err != nil {
				t.Fatal(err)
			}
		},
		"corrupt": func(t *testing.T, c *CatalogCache) {
			if err := os.WriteFile(path(c), []byte(`{"version": 1, "packages": [`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"another version": func(t *testing.T, c *CatalogCache) {
			b, _ := os.ReadFile(path(c))
			if err := os.WriteFile(path(c), bytes.Replace(b, []byte(`"version":1`), []byte(`"version":2`), 1), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"another host": func(t *testing.T, c *CatalogCache) {
			runBrew(t, brewCatalogServer(t), c)
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := testCatalogs(t)
			srv := brewCatalogServer(t)
			runBrew(t, srv, c)
			spoil(t, c)
			if m := runBrew(t, srv, c); len(m) != 3 || len(srv.Requests()) != 4 {
				t.Errorf("%d matches after %d requests; want the catalogs downloaded again", len(m), len(srv.Requests()))
			}
			// The download replaced it.
			runBrew(t, srv, c)
			if n := len(srv.Requests()); n != 4 {
				t.Errorf("%d requests; the new download should have been kept", n)
			}
		})
	}
}

func TestHomebrewNeverKeepsAFailedDownload(t *testing.T) {
	c := testCatalogs(t)
	wantSearchError(t, func() *Homebrew { a := brewFor(newMockServer(t, respond(500, ""))); a.Catalogs = c; return a }(),
		model.Query{Idea: "x", Keywords: []string{"json"}})
	if _, err := os.Stat(c.Dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("after a failed download: %v; want no catalog directory", err)
	}
}

func TestHomebrewWithoutACatalogCacheDownloadsEveryRun(t *testing.T) {
	for _, c := range []*CatalogCache{nil, {Dir: t.TempDir(), TTL: 0}} {
		srv := brewCatalogServer(t)
		runBrew(t, srv, c)
		runBrew(t, srv, c)
		if n := len(srv.Requests()); n != 4 {
			t.Errorf("%+v: %d requests for two runs, want 4", c, n)
		}
		if c != nil {
			if entries, _ := os.ReadDir(c.Dir); len(entries) != 0 {
				t.Errorf("TTL 0 wrote %d files", len(entries))
			}
		}
	}
}

func TestSearcherGivesHomebrewTheCatalogCache(t *testing.T) {
	c := testCatalogs(t)
	if a := (Searcher{Catalogs: c}).newAdapter(model.Homebrew, nil).(*Homebrew); a.Catalogs != c || a.catalog != sharedBrewCatalog {
		t.Errorf("adapter %+v; want the catalog cache and the shared in-memory catalog", a)
	}
}

func TestNewHomebrewSharesItsCatalog(t *testing.T) {
	if a, b := NewHomebrew(nil), NewHomebrew(nil); a.catalog != b.catalog {
		t.Error("live adapters must share one catalog")
	}
}

// ── Packagist ─────────────────────────────────────────────────────────────

const packagistBody = `{"results": [
  {"name": "laravel/framework", "description": "The Laravel Framework.", "url": "https://packagist.org/packages/laravel/framework",
   "repository": "https://github.com/laravel/framework", "downloads": 543583479, "favers": 35281},
  {"name": "symfony/cache", "description": "Provides extended PSR-6, PSR-16 (and tags) implementations", "url": "https://packagist.org/packages/symfony/cache",
   "repository": "https://github.com/symfony/cache", "downloads": 373170138, "favers": 4189}
], "total": 2}`

func packagistFor(srv *mockServer) *Packagist {
	return &Packagist{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func packagistServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/search.json", respond(status, body)))
}

func TestPackagistIDIsPackagist(t *testing.T) {
	if got := NewPackagist(nil).ID(); got != model.Packagist {
		t.Fatalf("ID() = %v", got)
	}
}

func TestPackagistMapsResultsIntoMatches(t *testing.T) {
	m := mustSearch(t, packagistFor(packagistServer(t, 200, packagistBody)), query())
	if len(m) != 2 || m[0].Name != "laravel/framework" || m[0].Source != model.Packagist ||
		m[0].URL != "https://packagist.org/packages/laravel/framework" || m[0].Description != "The Laravel Framework." ||
		deref(m[0].Popularity) != uint64(543_583_479) || m[0].Similarity != 0 ||
		m[1].Name != "symfony/cache" || deref(m[1].Popularity) != uint64(373_170_138) {
		t.Errorf("matches = %+v", m)
	}
}

func TestPackagistSendsJoinedKeywordsAndPerPage(t *testing.T) {
	srv := packagistServer(t, 200, packagistBody)
	mustSearch(t, packagistFor(srv), query())
	if params := srv.Requests()[0].URL.Query(); params.Get("q") != "async runtime" || params.Get("per_page") != "15" || len(srv.Requests()) != 1 {
		t.Errorf("params = %v", params)
	}
}

func TestPackagistFallsBackToFaversWhenDownloadsZero(t *testing.T) {
	m := mustSearch(t, packagistFor(packagistServer(t, 200, `{"results": [{"name": "fresh/package", "description": "Brand new.", "url": "u", "downloads": 0, "favers": 42}]}`)), query())
	if deref(m[0].Popularity) != uint64(42) {
		t.Errorf("popularity = %v", deref(m[0].Popularity))
	}
}

func TestPackagistMissingPopularityBecomesNone(t *testing.T) {
	m := mustSearch(t, packagistFor(packagistServer(t, 200, `{"results": [{"name": "obscure/pkg", "description": "no counts here", "url": "u"}]}`)), query())
	if m[0].Popularity != nil {
		t.Errorf("popularity = %v", deref(m[0].Popularity))
	}
}

func TestPackagistEmptyResultsIsOKNotError(t *testing.T) {
	if m := mustSearch(t, packagistFor(packagistServer(t, 200, `{"results": [], "total": 0}`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestPackagistServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, packagistFor(packagistServer(t, 500, "")), query())
}

func TestPackagistNarrowsWhenTheFullQueryIsEmpty(t *testing.T) {
	srv := narrowingServer(t, "/search.json", "q", narrowingSteps[:1], `{"results": []}`, "spell syntax mistakes", packagistBody)
	if m := mustSearch(t, packagistFor(srv), narrowingQuery()); len(m) != 2 || m[0].Name != "laravel/framework" {
		t.Errorf("matches = %+v after %q", m, requested(srv, "q"))
	}
}

func TestPackagistLinksToItsOwnPageWithoutAURL(t *testing.T) {
	srv := packagistServer(t, 200, `{"results": [{"name": "a/b", "favers": 0}]}`)
	m := mustSearch(t, packagistFor(srv), query())
	if m[0].URL != srv.URL+"/packages/a/b" || m[0].Popularity != nil {
		t.Errorf("match = %+v", m[0])
	}
}

// ── Hex ───────────────────────────────────────────────────────────────────

const hexBody = `[
  {"name": "phoenix", "updated_at": "2026-03-24T16:51:48.689517Z", "html_url": "https://hex.pm/packages/phoenix",
   "meta": {"description": "Productive web framework that does not compromise speed or maintainability."},
   "downloads": {"all": 50000000, "recent": 1000000, "week": 250000}},
  {"name": "cachex", "html_url": "https://hex.pm/packages/cachex",
   "meta": {"description": "A powerful caching library for Elixir."}, "downloads": {"all": 12345}}
]`

func hexFor(srv *mockServer) *Hex { return &Hex{Client: NewHTTPClient(), BaseURL: srv.URL} }

func hexServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/api/packages", respond(status, body)))
}

func TestHexIDIsHex(t *testing.T) {
	if got := NewHex(nil).ID(); got != model.Hex {
		t.Fatalf("ID() = %v", got)
	}
}

func TestHexMapsPackagesIntoMatches(t *testing.T) {
	m := mustSearch(t, hexFor(hexServer(t, 200, hexBody)), query())
	if len(m) != 2 || m[0].Name != "phoenix" || m[0].Source != model.Hex || m[0].URL != "https://hex.pm/packages/phoenix" ||
		m[0].Description != "Productive web framework that does not compromise speed or maintainability." ||
		deref(m[0].Popularity) != uint64(50_000_000) || m[1].Name != "cachex" || deref(m[1].Popularity) != uint64(12_345) {
		t.Errorf("matches = %+v", m)
	}
}

func TestHexSearchesEachKeywordOnItsOwnAndDedupesAcrossTerms(t *testing.T) {
	// A joined multi-word query returns the unfiltered catalogue, so each
	// keyword goes out alone, longest first, and a package both terms hit
	// appears once.
	srv := hexServer(t, 200, hexBody)
	m := mustSearch(t, hexFor(srv), query())
	if len(m) != 2 {
		t.Errorf("got %d matches; the same two packages from both terms, deduped", len(m))
	}
	if got := requested(srv, "search"); !slices.Equal(got, []string{"runtime", "async"}) {
		t.Errorf("searched %q", got)
	}
	if got := srv.Requests()[0].URL.Query().Get("page"); got != "1" {
		t.Errorf("page = %q", got)
	}
}

func TestHexSearchesOnlyTheThreeLongestKeywords(t *testing.T) {
	srv := hexServer(t, 200, `[]`)
	if m := mustSearch(t, hexFor(srv), narrowingQuery()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
	if got := requested(srv, "search"); !slices.Equal(got, []string{"mistakes", "syntax", "spell"}) {
		t.Errorf("searched %q", got)
	}
}

func TestHexSearchesTheIdeaWithoutKeywords(t *testing.T) {
	srv := hexServer(t, 200, `[]`)
	mustSearch(t, hexFor(srv), model.Query{Idea: "elixir caching"})
	if got := requested(srv, "search"); !slices.Equal(got, []string{"elixir caching"}) {
		t.Errorf("searched %q", got)
	}
}

func TestHexMissingDescriptionBecomesEmpty(t *testing.T) {
	m := mustSearch(t, hexFor(hexServer(t, 200, `[{"name": "bare", "html_url": "https://hex.pm/packages/bare", "downloads": {"all": 5}}]`)), query())
	if len(m) != 1 || m[0].Description != "" {
		t.Errorf("matches = %+v", m)
	}
}

func TestHexZeroOrMissingDownloadsBecomesNoneAndURLFallsBack(t *testing.T) {
	srv := hexServer(t, 200, `[
	  {"name": "mystery", "meta": {"description": "no link, no downloads"}, "downloads": {"all": 0}},
	  {"name": "ghost", "meta": {"description": "no downloads block at all"}, "html_url": "  "}
	]`)
	m := mustSearch(t, hexFor(srv), query())
	if len(m) != 2 || m[0].Popularity != nil || m[0].URL != srv.URL+"/packages/mystery" || m[1].Popularity != nil || m[1].URL != srv.URL+"/packages/ghost" {
		t.Errorf("matches = %+v", m)
	}
}

func TestHexEmptyResultsIsOKNotError(t *testing.T) {
	if m := mustSearch(t, hexFor(hexServer(t, 200, `[]`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestHexServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, hexFor(hexServer(t, 500, "")), query())
}

func TestHexNormalisesMicrosecondDates(t *testing.T) {
	m := mustSearch(t, hexFor(hexServer(t, 200, hexBody)), query())
	if deref(m[0].LastUpdated) != "2026-03-24T16:51:48Z" || m[1].LastUpdated != nil {
		t.Errorf("dates = %v, %v", deref(m[0].LastUpdated), deref(m[1].LastUpdated))
	}
}

func TestHexMalformedDateNeverFailsTheSource(t *testing.T) {
	body := strings.Replace(hexBody, `"2026-03-24T16:51:48.689517Z"`, `{"iso": "2026-03-24"}`, 1) // object, was a string
	if m := mustSearch(t, hexFor(hexServer(t, 200, body)), query()); len(m) != 2 || m[0].LastUpdated != nil {
		t.Errorf("matches = %+v", m)
	}
}

// ── Artifact Hub ──────────────────────────────────────────────────────────

const artifacthubBody = `{"packages": [
  {"name": "prometheus", "ts": 1785445235, "description": "Prometheus monitoring system and time series database", "stars": 4200,
   "repository": {"name": "prometheus-community", "kind": 0, "url": "https://prometheus-community.github.io/helm-charts"}},
  {"name": "trace-dns", "stars": 0,
   "repository": {"name": "gadgets", "kind": 22, "url": "https://github.com/inspektor-gadget/inspektor-gadget"}}
]}`

func artifacthubFor(srv *mockServer) *ArtifactHub {
	return &ArtifactHub{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func artifacthubServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/api/v1/packages/search", respond(status, body)))
}

func TestArtifactHubIDIsArtifactHub(t *testing.T) {
	if got := NewArtifactHub(nil).ID(); got != model.ArtifactHub {
		t.Fatalf("ID() = %v", got)
	}
}

func TestArtifactHubMapsPackagesIntoMatches(t *testing.T) {
	srv := artifacthubServer(t, 200, artifacthubBody)
	m := mustSearch(t, artifacthubFor(srv), query())
	if len(m) != 2 || m[0].Name != "prometheus" || m[0].Source != model.ArtifactHub ||
		m[0].URL != srv.URL+"/packages/helm/prometheus-community/prometheus" ||
		m[0].Description != "Prometheus monitoring system and time series database" || deref(m[0].Popularity) != uint64(4200) {
		t.Errorf("first = %+v", m[0])
	}
	if m[1].Name != "trace-dns" || m[1].URL != srv.URL+"/packages/inspektor-gadget/gadgets/trace-dns" || m[1].Description != "" || m[1].Popularity != nil {
		t.Errorf("second = %+v", m[1])
	}
}

func TestArtifactHubSendsJoinedKeywordsAsQuery(t *testing.T) {
	srv := artifacthubServer(t, 200, artifacthubBody)
	mustSearch(t, artifacthubFor(srv), query())
	r := srv.Requests()[0]
	if p := r.URL.Query(); p.Get("ts_query_web") != "async runtime" || p.Get("limit") != "15" || p.Get("facets") != "false" || r.UserAgent() == "" {
		t.Errorf("request = %v", r.URL)
	}
}

func TestArtifactHubUnknownKindFallsBackToRepositoryURL(t *testing.T) {
	m := mustSearch(t, artifacthubFor(artifacthubServer(t, 200, `{"packages": [{"name": "future-thing", "stars": 3,
	  "repository": {"name": "some-repo", "kind": 9999, "url": "https://example.com/some-repo"}}]}`)), query())
	if len(m) != 1 || m[0].URL != "https://example.com/some-repo" {
		t.Errorf("matches = %+v", m)
	}
}

func TestArtifactHubEmptyResultsIsOK(t *testing.T) {
	for _, body := range []string{`{"packages": []}`, `{}`} {
		if m := mustSearch(t, artifacthubFor(artifacthubServer(t, 200, body)), query()); len(m) != 0 {
			t.Errorf("%s: matches = %+v", body, m)
		}
	}
}

func TestArtifactHubServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, artifacthubFor(artifacthubServer(t, 500, "")), query())
}

func TestArtifactHubConvertsEpochSeconds(t *testing.T) {
	m := mustSearch(t, artifacthubFor(artifacthubServer(t, 200, artifacthubBody)), query())
	if deref(m[0].LastUpdated) != "2026-07-30T21:00:35Z" || m[1].LastUpdated != nil {
		t.Errorf("dates = %v, %v", deref(m[0].LastUpdated), deref(m[1].LastUpdated))
	}
}

func TestArtifactHubMalformedDateNeverFailsTheSource(t *testing.T) {
	body := strings.Replace(artifacthubBody, "1785445235", `"not-a-number"`, 1)
	if m := mustSearch(t, artifacthubFor(artifacthubServer(t, 200, body)), query()); len(m) != 2 || m[0].LastUpdated != nil {
		t.Errorf("matches = %+v", m)
	}
}

func TestArtifactHubNarrowsAllTheWayToASingleKeyword(t *testing.T) {
	srv := narrowingServer(t, "/api/v1/packages/search", "ts_query_web", narrowingSteps[:3], `{"packages": []}`, "mistakes", artifacthubBody)
	if m := mustSearch(t, artifacthubFor(srv), narrowingQuery()); len(m) != 2 || m[0].Name != "prometheus" {
		t.Errorf("matches = %+v after %q", m, requested(srv, "ts_query_web"))
	}
}

// ── AUR ───────────────────────────────────────────────────────────────────

const aurBody = `{"resultcount": 2, "results": [
  {"Name": "ccache", "LastModified": 1778477360, "Description": "Compiler cache that speeds up recompilation",
   "URL": "https://ccache.dev", "NumVotes": 120, "Popularity": 3.5, "Maintainer": "someone", "Version": "4.10-1"},
  {"Name": "sccache-git", "Description": null, "URL": null, "NumVotes": 0, "Popularity": 0, "Maintainer": null, "Version": "0.8.1-1"}
]}`

func aurFor(srv *mockServer) *AUR { return &AUR{Client: NewHTTPClient(), BaseURL: srv.URL} }

func aurServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/rpc/", respond(status, body)))
}

func TestAURIDIsAUR(t *testing.T) {
	if got := NewAUR(nil).ID(); got != model.AUR {
		t.Fatalf("ID() = %v", got)
	}
}

func TestAURMapsPackagesIntoMatches(t *testing.T) {
	srv := aurServer(t, 200, aurBody)
	m := mustSearch(t, aurFor(srv), query())
	if len(m) != 2 || m[0].Name != "ccache" || m[0].Source != model.AUR || m[0].URL != srv.URL+"/packages/ccache" ||
		m[0].Description != "Compiler cache that speeds up recompilation" || deref(m[0].Popularity) != uint64(120) {
		t.Errorf("first = %+v", m[0])
	}
	if m[1].Name != "sccache-git" || m[1].URL != srv.URL+"/packages/sccache-git" || m[1].Description != "" || m[1].Popularity != nil {
		t.Errorf("second = %+v", m[1])
	}
}

func TestAURSendsSearchQueryParams(t *testing.T) {
	srv := aurServer(t, 200, aurBody)
	mustSearch(t, aurFor(srv), query())
	p := srv.Requests()[0].URL.Query()
	if p.Get("v") != "5" || p.Get("type") != "search" || p.Get("by") != "name-desc" || p.Get("arg") != "async runtime" || len(srv.Requests()) != 1 {
		t.Errorf("params = %v", p)
	}
}

func TestAUREmptyResultsIsOK(t *testing.T) {
	if m := mustSearch(t, aurFor(aurServer(t, 200, `{"resultcount": 0, "results": []}`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestAURServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, aurFor(aurServer(t, 500, "")), query())
}

const aurTooMany = `{"error": "Too many package results.", "resultcount": 0, "results": [], "type": "error"}`

// The RPC refuses over-broad terms with HTTP 200 and an error body. Name
// matching survives terms that name+description matching is refused for, so
// the refusal costs recall for that step, not the whole source.
func TestAURTooManyResultsRetriesAgainstNamesOnly(t *testing.T) {
	srv := newMockServer(t, onPath("/rpc/", byParam("by", map[string]http.Handler{
		"name-desc": respond(200, aurTooMany),
		"name":      respond(200, aurBody),
	})))
	m := mustSearch(t, aurFor(srv), query())
	if len(m) != 2 || m[0].Name != "ccache" || !slices.Equal(requested(srv, "by"), []string{"name-desc", "name"}) {
		t.Errorf("matches = %+v after %q", m, requested(srv, "by"))
	}
}

func TestAURRefusedEvenByNameIsUnavailable(t *testing.T) {
	_, err := aurFor(aurServer(t, 200, aurTooMany)).Search(t.Context(), query())
	var unavailable *silt.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("err = %v", err)
	}
}

// An RPC error the fallback cannot get past must reach the caller as a
// failed source: decoding only results would report the refusal as a
// successful search that found nothing — the one thing this tool must never
// claim.
func TestAURErrorBodyFailsTheSourceInsteadOfReportingEmpty(t *testing.T) {
	_, err := aurFor(aurServer(t, 200, `{"error": "Incorrect request type specified.", "resultcount": 0, "results": [], "type": "error"}`)).Search(t.Context(), query())
	var unavailable *silt.UnavailableError
	if !errors.As(err, &unavailable) || err.Error() != "AUR search: Incorrect request type specified." {
		t.Errorf("a 200 carrying an RPC error must not decode to an empty result, got: %v", err)
	}
}

// A single broad keyword matches hundreds of unranked AUR packages, so each
// step keeps only the most voted-for handful.
func TestAURKeepsOnlyTheTopVotedPackages(t *testing.T) {
	var results []map[string]any
	for i := range 25 {
		results = append(results, map[string]any{"Name": fmt.Sprintf("pkg-%d", i), "Description": "a package", "NumVotes": i})
	}
	body, _ := json.Marshal(map[string]any{"resultcount": 25, "results": results})
	m := mustSearch(t, aurFor(aurServer(t, 200, string(body))), query())
	if len(m) != 20 || m[0].Name != "pkg-24" || m[19].Name != "pkg-5" {
		t.Errorf("got %d matches: %v first, %v last", len(m), m[0].Name, m[len(m)-1].Name)
	}
}

func TestAURConvertsEpochSeconds(t *testing.T) {
	m := mustSearch(t, aurFor(aurServer(t, 200, aurBody)), query())
	if deref(m[0].LastUpdated) != "2026-05-11T05:29:20Z" || m[1].LastUpdated != nil {
		t.Errorf("dates = %v, %v", deref(m[0].LastUpdated), deref(m[1].LastUpdated))
	}
}

func TestAURMalformedDateNeverFailsTheSource(t *testing.T) {
	body := strings.Replace(aurBody, "1778477360", "9223372036854775807", 1) // out of range
	if m := mustSearch(t, aurFor(aurServer(t, 200, body)), query()); len(m) != 2 || m[0].LastUpdated != nil {
		t.Errorf("matches = %+v", m)
	}
}

// The AUR matches the whole argument against one package, so like Maven and
// NuGet it only recovers at a single keyword.
func TestAURNarrowsAllTheWayToASingleKeyword(t *testing.T) {
	srv := narrowingServer(t, "/rpc/", "arg", narrowingSteps[:3], `{"resultcount": 0, "results": [], "type": "search"}`, "mistakes", aurBody)
	if m := mustSearch(t, aurFor(srv), narrowingQuery()); len(m) != 2 || m[0].Name != "ccache" {
		t.Errorf("matches = %+v after %q", m, requested(srv, "arg"))
	}
}

// ── Hackage ───────────────────────────────────────────────────────────────

func hackageFor(srv *mockServer) *Hackage { return &Hackage{Client: NewHTTPClient(), BaseURL: srv.URL} }

func hackageServer(t *testing.T, search string, cabal map[string]string) *mockServer {
	return newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/packages/search" {
			respond(200, search)(w, r)
			return
		}
		if body, ok := cabal[r.URL.Path]; ok {
			w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestHackageIDIsHackage(t *testing.T) {
	if got := NewHackage(nil).ID(); got != model.Hackage {
		t.Fatalf("ID() = %v", got)
	}
}

func TestHackageReadsSynopsisAndHomepageFromTheCabalFile(t *testing.T) {
	srv := hackageServer(t, `[{"name": "aeson"}, {"name": "gone"}]`, map[string]string{
		"/package/aeson/aeson.cabal": "cabal-version: 2.2\r\nname:          aeson\r\n  Synopsis:  Fast JSON parsing and encoding\r\nhomepage:      https://github.com/haskell/aeson\r\n",
	})
	m := mustSearch(t, hackageFor(srv), query())
	if len(m) != 2 {
		t.Fatalf("matches = %+v", m)
	}
	if m[0].Name != "aeson" || m[0].Source != model.Hackage || m[0].Description != "Fast JSON parsing and encoding" || m[0].URL != "https://github.com/haskell/aeson" || m[0].LastUpdated != nil {
		t.Errorf("aeson = %+v", m[0])
	}
	// A .cabal file that can't be fetched only costs the description and
	// homepage.
	if m[1].Name != "gone" || m[1].Description != "" || m[1].URL != srv.URL+"/package/gone" {
		t.Errorf("gone = %+v", m[1])
	}
	r := srv.Requests()[0]
	if r.URL.Query().Get("terms") != "async runtime" || r.Header.Get("Accept") != "application/json" {
		t.Errorf("search request = %v", r.URL)
	}
}

func TestHackageFetchesAtMost20CabalFiles(t *testing.T) {
	var names []string
	for i := range 25 {
		names = append(names, fmt.Sprintf(`{"name": "p%d"}`, i))
	}
	srv := hackageServer(t, "["+strings.Join(names, ",")+"]", nil)
	if m := mustSearch(t, hackageFor(srv), query()); len(m) != 20 || m[19].Name != "p19" {
		t.Errorf("got %d matches", len(m))
	}
	if n := len(srv.Requests()); n != 21 {
		t.Errorf("%d requests, want 1 search and 20 .cabal files", n)
	}
}

func TestHackageErrors(t *testing.T) {
	wantSearchError(t, hackageFor(newMockServer(t, respond(500, ""))), query())
	wantSearchError(t, hackageFor(hackageServer(t, `[{"nam": "typo"}]`, nil)), query())
	if m := mustSearch(t, hackageFor(hackageServer(t, `[]`, nil)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestCabalField(t *testing.T) {
	cabal := "name: x\nsynopsis:\nSynopsis: second\nhomepage-extra: no\n"
	if got := cabalField(cabal, "synopsis"); got != "second" {
		t.Errorf("synopsis = %q; an empty value is skipped", got)
	}
	// Ported as-is: a field name is matched as a prefix.
	if got := cabalField(cabal, "homepage"); got != "no" {
		t.Errorf("homepage = %q", got)
	}
}

// ── Nixpkgs ───────────────────────────────────────────────────────────────

const nixpkgsBody = `{"hits": {"hits": [
  {"_source": {"package_attr_name": "ripgrep", "package_pname": "ripgrep",
    "package_description": "A utility that combines the usability of The Silver Searcher with the raw speed of grep",
    "package_homepage": ["https://github.com/BurntSushi/ripgrep"], "package_pversion": "14.1.0"}},
  {"_source": {"package_attr_name": "fd", "package_pname": "fd",
    "package_description": "A simple, fast and user-friendly alternative to find",
    "package_homepage": ["https://github.com/sharkdp/fd"], "package_pversion": "10.2.0"}}
]}}`

func nixpkgsFor(srv *mockServer) *Nixpkgs { return &Nixpkgs{Client: NewHTTPClient(), BaseURL: srv.URL} }

func nixpkgsServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/backend/latest-50-nixos-unstable/_search", respond(status, body)))
}

func TestNixpkgsIDIsNixpkgs(t *testing.T) {
	if got := NewNixpkgs(nil).ID(); got != model.Nixpkgs {
		t.Fatalf("ID() = %v", got)
	}
}

func TestNixpkgsSearchReturnsMatches(t *testing.T) {
	m := mustSearch(t, nixpkgsFor(nixpkgsServer(t, 200, nixpkgsBody)), query())
	if len(m) != 2 || m[0].Name != "ripgrep (v14.1.0)" || m[0].Source != model.Nixpkgs || m[0].URL != "https://github.com/BurntSushi/ripgrep" ||
		m[0].Description != "A utility that combines the usability of The Silver Searcher with the raw speed of grep" || m[0].Popularity != nil {
		t.Errorf("first = %+v", m[0])
	}
	if m[1].Name != "fd (v10.2.0)" || m[1].URL != "https://github.com/sharkdp/fd" || m[1].Description != "A simple, fast and user-friendly alternative to find" {
		t.Errorf("second = %+v", m[1])
	}
}

// The backend 401s anonymous callers, so a request without credentials
// returns nothing at all — the failure that shipped in patent 0.9.0.
func TestNixpkgsSendsAuthorizationHeader(t *testing.T) {
	srv := nixpkgsServer(t, 200, nixpkgsBody)
	mustSearch(t, nixpkgsFor(srv), query())
	r := srv.Requests()[0]
	user, password, ok := r.BasicAuth()
	if !ok || user != nixpkgsUser || password != nixpkgsPassword || r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("request = %s %v", r.Method, r.Header)
	}
}

func TestNixpkgsSendsTheFrontendsQueryWithOr(t *testing.T) {
	var got map[string]any
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		respond(200, nixpkgsBody)(w, r)
	}))
	mustSearch(t, nixpkgsFor(srv), query())
	mm := got["query"].(map[string]any)["dis_max"].(map[string]any)["queries"].([]any)[0].(map[string]any)["multi_match"].(map[string]any)
	if mm["query"] != "async runtime" || mm["operator"] != "or" || got["size"] != 20.0 {
		t.Errorf("query = %v", got)
	}
}

func TestNixpkgsEmptyResults(t *testing.T) {
	if m := mustSearch(t, nixpkgsFor(nixpkgsServer(t, 200, `{"hits": {"hits": []}}`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestNixpkgsServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, nixpkgsFor(nixpkgsServer(t, 500, "")), query())
}

func TestNixpkgsMalformedBodyIsPropagated(t *testing.T) {
	for _, body := range []string{"<html>not json</html>", `{}`, `{"hits": {}}`, `{"hits": {"hits": [{"_source": {"package_pname": "x"}}]}}`} {
		wantSearchError(t, nixpkgsFor(nixpkgsServer(t, 200, body)), query())
	}
}

func TestNixpkgsFallsBackToTheSearchPageAndAttrName(t *testing.T) {
	m := mustSearch(t, nixpkgsFor(nixpkgsServer(t, 200, `{"hits": {"hits": [
	  {"_source": {"package_attr_name": "python3Packages.foo", "package_homepage": ["", "https://second"], "package_pversion": ""}}
	]}}`)), query())
	// Only the first homepage counts, and the link is built unescaped.
	if len(m) != 1 || m[0].Name != "python3Packages.foo" ||
		m[0].URL != "https://search.nixos.org/packages?channel=unstable&show=python3Packages.foo&query=python3Packages.foo" {
		t.Errorf("matches = %+v", m)
	}
}
