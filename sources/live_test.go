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
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/miku/silt"
	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

// live searches a for idea and keywords and checks the result; dated sources
// must also carry plausible dates.
func live(t *testing.T, a Adapter, idea string, keywords []string, dated bool) {
	t.Helper()
	m, err := a.Search(context.Background(), model.Query{Idea: idea, Keywords: keywords})
	if err != nil {
		t.Fatalf("%v live search errored: %v", a.ID(), err)
	}
	assertLive(t, m, a.ID())
	if dated {
		assertDated(t, m, a.ID())
	}
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

func client() *http.Client { return NewHTTPClient() }

func TestLiveCratesIO(t *testing.T) {
	live(t, NewCratesIO(client()), "a fast async runtime for rust", []string{"async", "runtime"}, true)
}

func TestLiveGitHub(t *testing.T) {
	live(t, NewGitHub(client()), "a kubernetes command line tool", []string{"kubernetes", "cli"}, true)
}

func TestLiveNpm(t *testing.T) {
	live(t, NewNpm(client()), "react component library", []string{"react"}, true)
}

// KNOWN, ACCEPTED DEGRADATION, as in patent: PyPI has retired every keyless
// search path, and the search page sits behind a bot wall that serves a JS
// stub to any non-browser client. The adapter fails honestly, as unavailable,
// so PyPI is reported as not reached. This test passes on that, and checks the
// results should a keyless backend ever appear.
func TestLivePyPI(t *testing.T) {
	m, err := NewPyPI(client()).Search(context.Background(), model.Query{Idea: "http requests library for python", Keywords: []string{"requests"}})
	var unavailable *silt.UnavailableError
	if errors.As(err, &unavailable) {
		t.Skipf("PyPI is walled, as expected: %v", err)
	}
	if err != nil {
		t.Fatalf("PyPI failed, but not with the expected wall: %v", err)
	}
	assertLive(t, m, model.PyPI)
}

func TestLiveHackerNews(t *testing.T) {
	live(t, NewHackerNews(client()), "database", []string{"database"}, false)
}

func TestLiveGo(t *testing.T) {
	live(t, NewGoPkgDev(client()), "web framework for go", []string{"gin"}, true)
}

func TestLiveMaven(t *testing.T) {
	live(t, NewMaven(client()), "json serialization library", []string{"jackson"}, true)
}

func TestLiveRubyGems(t *testing.T) {
	live(t, NewRubyGems(client()), "a ruby web framework", []string{"rails"}, false)
}

func TestLiveDockerHub(t *testing.T) {
	live(t, NewDockerHub(client()), "a web server container image", []string{"nginx"}, false)
}

func TestLiveVSCode(t *testing.T) {
	live(t, NewVSCodeMarketplace(client()), "python language support", []string{"python"}, true)
}

func TestLiveNuGet(t *testing.T) {
	live(t, NewNuGet(client()), "a json library", []string{"json"}, false)
}

func TestLiveHomebrew(t *testing.T) {
	live(t, NewHomebrew(client()), "download files from the web", []string{"wget"}, false)
}

func TestLivePackagist(t *testing.T) {
	live(t, NewPackagist(client()), "a php web framework", []string{"laravel"}, false)
}

func TestLiveHex(t *testing.T) {
	live(t, NewHex(client()), "an elixir web framework", []string{"phoenix"}, true)
}

func TestLiveArtifactHub(t *testing.T) {
	live(t, NewArtifactHub(client()), "prometheus monitoring helm chart", []string{"prometheus"}, true)
}

func TestLiveAUR(t *testing.T) {
	live(t, NewAUR(client()), "an aur helper", []string{"yay"}, true)
}

// The AUR refuses a term that matches too much with an HTTP 200 error body,
// which the adapter answers by matching names only. The mocks prove the
// handling but not the trigger: "library" is refused in the default
// name+description mode and must still come back with packages.
func TestLiveAURRecoversFromARefusedQuery(t *testing.T) {
	live(t, NewAUR(client()), "a shared library", []string{"library"}, false)
}

func TestLiveHackage(t *testing.T) {
	live(t, NewHackage(client()), "a haskell json parsing library", []string{"json"}, false)
}

func TestLiveJetBrains(t *testing.T) {
	live(t, NewJetBrains(client()), "a jetbrains plugin for kubernetes yaml files", []string{"plugin", "kubernetes", "yaml"}, true)
}

// Guards what a mocked test cannot: that the Elasticsearch backend still
// accepts the credentials (it 401s anonymous callers), and that the index
// generation in the URL still exists (upstream deletes old generations).
// ripgrep is a very stable attribute name, so an empty result means drift.
func TestLiveNixpkgs(t *testing.T) {
	live(t, NewNixpkgs(client()), "a fast recursive grep replacement", []string{"ripgrep"}, false)
}

// The keyword list the pipeline really sends: no single package is all of
// these, so this goes empty again the moment the query turns conjunctive.
func TestLiveNixpkgsMultiKeyword(t *testing.T) {
	live(t, NewNixpkgs(client()), "a nix flake tool for managing developer shells",
		[]string{"nix", "flake", "developer", "shell", "manager"}, false)
}
