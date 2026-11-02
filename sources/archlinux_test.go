package sources

import (
	"fmt"
	"strings"
	"testing"

	"github.com/miku/silt/model"
)

// Trimmed from a live response, in the server's order: by name.
const archLinuxBody = `{"version": 2, "limit": 250, "valid": true, "num_pages": 1, "page": 1, "count": 2, "results": [
  {"pkgname": "fkill", "pkgbase": "fkill", "repo": "extra", "arch": "any", "pkgver": "9.0.0", "pkgrel": "1",
   "pkgdesc": "Fabulously kill processes. Cross-platform.", "url": "https://github.com/sindresorhus/fkill-cli",
   "last_update": "2025-11-16T20:30:20.438Z"},
  {"pkgname": "tokio-console", "pkgbase": "tokio-console", "repo": "extra", "arch": "x86_64", "pkgver": "0.1.13", "pkgrel": "1",
   "pkgdesc": "A debugger for async Rust programs", "url": "https://github.com/tokio-rs/console",
   "last_update": "2026-03-01T08:00:00.000Z"}
]}`

func archLinuxFor(srv *mockServer) *ArchLinux {
	return &ArchLinux{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func archLinuxServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/packages/search/json/", respond(status, body)))
}

// archLinuxResults wraps packages, given as JSON objects, in a response.
func archLinuxResults(pkgs ...string) string {
	return `{"version": 2, "valid": true, "results": [` + strings.Join(pkgs, ", ") + `]}`
}

func archLinuxPkg(name, repo, desc string) string {
	return fmt.Sprintf(`{"pkgname": %q, "repo": %q, "arch": "x86_64", "pkgdesc": %q, "last_update": "2026-01-01T00:00:00Z"}`, name, repo, desc)
}

func TestArchLinuxIDIsArchLinux(t *testing.T) {
	if got := NewArchLinux(nil).ID(); got != model.ArchLinux {
		t.Fatalf("ID() = %v", got)
	}
}

func TestArchLinuxMapsPackagesIntoMatches(t *testing.T) {
	srv := archLinuxServer(t, 200, archLinuxBody)
	m := mustSearch(t, archLinuxFor(srv), query())
	if len(m) != 2 {
		t.Fatalf("matches = %+v", m)
	}
	// tokio-console holds one of the keywords (async) and fkill none, so
	// tokio-console comes first, although the server listed it second.
	if m[0].Name != "tokio-console" || m[0].Source != model.ArchLinux || m[0].URL != srv.URL+"/packages/extra/x86_64/tokio-console/" ||
		m[0].Description != "A debugger for async Rust programs" || m[0].Popularity != nil || deref(m[0].LastUpdated) != "2026-03-01T08:00:00Z" {
		t.Errorf("first = %+v", m[0])
	}
	if m[1].Name != "fkill" || m[1].URL != srv.URL+"/packages/extra/any/fkill/" || deref(m[1].LastUpdated) != "2025-11-16T20:30:20Z" {
		t.Errorf("second = %+v", m[1])
	}
}

func TestArchLinuxSendsSearchQueryParams(t *testing.T) {
	srv := archLinuxServer(t, 200, archLinuxBody)
	mustSearch(t, archLinuxFor(srv), query())
	if p := srv.Requests()[0].URL.Query(); p.Get("q") != "async runtime" || len(p) != 1 || len(srv.Requests()) != 1 {
		t.Errorf("params = %v", p)
	}
}

func TestArchLinuxEmptyResultsIsOK(t *testing.T) {
	if m := mustSearch(t, archLinuxFor(archLinuxServer(t, 200, archLinuxResults())), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestArchLinuxServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, archLinuxFor(archLinuxServer(t, 500, "")), query())
}

func TestArchLinuxChangedResponseShapeFailsTheSource(t *testing.T) {
	for _, body := range []string{
		`{"version": 2, "valid": true}`,
		strings.Replace(archLinuxBody, `"pkgname": "fkill", `, "", 1),
		strings.Replace(archLinuxBody, `"repo": "extra", "arch": "any", `, `"arch": "any", `, 1),
		strings.Replace(archLinuxBody, `"arch": "any", `, "", 1),
	} {
		wantSearchError(t, archLinuxFor(archLinuxServer(t, 200, body)), query())
	}
}

func TestArchLinuxMalformedDateNeverFailsTheSource(t *testing.T) {
	body := strings.Replace(archLinuxBody, `"2025-11-16T20:30:20.438Z"`, `"last week"`, 1)
	if m := mustSearch(t, archLinuxFor(archLinuxServer(t, 200, body)), query()); len(m) != 2 || m[1].LastUpdated != nil {
		t.Errorf("matches = %+v", m)
	}
}

func TestArchLinuxNarrowsAllTheWayToASingleKeyword(t *testing.T) {
	srv := narrowingServer(t, "/packages/search/json/", "q", narrowingSteps[:3], archLinuxResults(), "mistakes", archLinuxBody)
	if m := mustSearch(t, archLinuxFor(srv), narrowingQuery()); len(m) != 2 {
		t.Errorf("matches = %+v after %q", m, requested(srv, "q"))
	}
}

func TestArchLinuxKeepsOnePackagePreferringTheStableRepository(t *testing.T) {
	// The search lists a package once per repository it is in. The stable
	// one wins whichever comes first; a package only in testing is kept.
	body := archLinuxResults(
		archLinuxPkg("argocd", "extra-testing", "Declarative continuous deployment, next"),
		archLinuxPkg("argocd", "extra", "Declarative continuous deployment"),
		archLinuxPkg("argocd", "core-staging", "Declarative continuous deployment, staged"),
		archLinuxPkg("brand-new", "extra-testing", "Not released yet"),
	)
	m := mustSearch(t, archLinuxFor(archLinuxServer(t, 200, body)), query())
	if len(m) != 2 || m[0].Description != "Declarative continuous deployment" || !strings.Contains(m[0].URL, "/packages/extra/x86_64/argocd/") ||
		m[1].Name != "brand-new" {
		t.Errorf("matches = %+v", m)
	}
}

func TestArchLinuxKeepsThePackagesWithTheMostKeywords(t *testing.T) {
	// One broad keyword matches hundreds of packages, listed by name: the
	// ones that hold more of the idea's keywords are kept, and otherwise the
	// server's order. A keyword counts at the start of a word, stemmed
	// ("kills" counts for "kill"), not inside one ("port" in "support").
	var pkgs []string
	for i := range 30 {
		pkgs = append(pkgs, archLinuxPkg(fmt.Sprintf("a%02d", i), "extra", "a tool to process images, with export support"))
	}
	pkgs = append(pkgs, archLinuxPkg("zz-fkill", "extra", "Fabulously kill processes"))
	q := model.Query{Idea: "a tool that kills a process on a port", Keywords: []string{"kills", "process", "port"}}
	m := mustSearch(t, archLinuxFor(archLinuxServer(t, 200, archLinuxResults(pkgs...))), q)
	if len(m) != archLinuxMaxHits || m[0].Name != "zz-fkill" || m[1].Name != "a00" || m[archLinuxMaxHits-1].Name != "a18" {
		t.Errorf("got %d matches, first %q, second %q, last %q", len(m), m[0].Name, m[1].Name, m[len(m)-1].Name)
	}
}
