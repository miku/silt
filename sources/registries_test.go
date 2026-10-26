package sources

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/miku/silt/model"
)

// The narrowing steps of narrowingQuery, longest first.
var narrowingSteps = []string{"ide code spell syntax mistakes", "spell syntax mistakes", "syntax mistakes", "mistakes"}

// ── Maven ─────────────────────────────────────────────────────────────────

const mavenBody = `{"response": {"docs": [
  {"g": "com.google.guava", "a": "guava", "versionCount": 50, "timestamp": 1750337811233}
]}}`

func mavenFor(srv *mockServer) *Maven { return &Maven{Client: NewHTTPClient(), BaseURL: srv.URL} }

func mavenServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/solrsearch/select", respond(status, body)))
}

func TestMavenIDIsMaven(t *testing.T) {
	if got := NewMaven(nil).ID(); got != model.Maven {
		t.Fatalf("ID() = %v", got)
	}
}

func TestMavenMapsArtifactsIntoMatches(t *testing.T) {
	srv := mavenServer(t, 200, mavenBody)
	m := mustSearch(t, mavenFor(srv), query())
	// Maven Central has no popularity signal comparable to the other
	// sources', so popularity stays nil.
	if len(m) != 1 || m[0].Name != "com.google.guava:guava" || m[0].Source != model.Maven ||
		m[0].URL != "https://central.sonatype.com/artifact/com.google.guava/guava" ||
		m[0].Popularity != nil || m[0].Description != "guava" {
		t.Errorf("matches = %+v", m)
	}
	params := srv.Requests()[0].URL.Query()
	if params.Get("q") != "async runtime" || params.Get("rows") != "20" || params.Get("wt") != "json" {
		t.Errorf("params = %v", params)
	}
}

func TestMavenIgnoresVersionCountWhenMappingResults(t *testing.T) {
	// versionCount is a valid Solr field, but surfacing it as popularity
	// would mislead the TUI's popularity column.
	m := mustSearch(t, mavenFor(mavenServer(t, 200, `{"response": {"docs": [{"g": "com.google.guava", "a": "guava", "versionCount": 999999}]}}`)), query())
	if m[0].Popularity != nil {
		t.Errorf("popularity = %v", *m[0].Popularity)
	}
}

func TestMavenServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, mavenFor(mavenServer(t, 503, "")), query())
}

func TestMavenEmptyResultsIsOK(t *testing.T) {
	if m := mustSearch(t, mavenFor(mavenServer(t, 200, `{"response": {"docs": []}}`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestMavenChangedResponseShapeIsAnError(t *testing.T) {
	for _, body := range []string{`{}`, `{"response": {}}`, `{"response": {"docs": [{"g": 1}]}}`} {
		wantSearchError(t, mavenFor(mavenServer(t, 200, body)), query())
	}
}

func TestMavenConvertsEpochMillis(t *testing.T) {
	// Millis, not seconds: read as seconds, 1750337811233 lands in year 57000.
	m := mustSearch(t, mavenFor(mavenServer(t, 200, mavenBody)), query())
	if deref(m[0].LastUpdated) != "2025-06-19T12:56:51Z" {
		t.Errorf("last_updated = %v", deref(m[0].LastUpdated))
	}
}

func TestMavenMalformedDateNeverFailsTheSource(t *testing.T) {
	body := strings.Replace(mavenBody, "1750337811233", `"1750337811233"`, 1) // string, was a number
	if m := mustSearch(t, mavenFor(mavenServer(t, 200, body)), query()); len(m) != 1 || m[0].LastUpdated != nil {
		t.Errorf("matches = %+v", m)
	}
}

// Maven is the strict one: its Solr index matches artifact coordinates only,
// so even two terms come back empty and it has to fall all the way to one.
func TestMavenNarrowsAllTheWayToASingleKeyword(t *testing.T) {
	srv := narrowingServer(t, "/solrsearch/select", "q", narrowingSteps[:3], `{"response": {"docs": []}}`, "mistakes", mavenBody)
	m := mustSearch(t, mavenFor(srv), narrowingQuery())
	if len(m) != 1 || m[0].Name != "com.google.guava:guava" || !slices.Equal(requested(srv, "q"), narrowingSteps) {
		t.Errorf("matches = %+v after %q", m, requested(srv, "q"))
	}
}

// ── NuGet ─────────────────────────────────────────────────────────────────

const nugetBody = `{"data": [
  {"id": "Newtonsoft.Json", "description": "JSON framework for .NET", "totalDownloads": 100},
  {"id": "NoDescription", "description": "", "totalDownloads": 1}
]}`

func nugetFor(srv *mockServer) *NuGet { return &NuGet{Client: NewHTTPClient(), BaseURL: srv.URL} }

func nugetServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/query", respond(status, body)))
}

func TestNuGetIDIsNuGet(t *testing.T) {
	if got := NewNuGet(nil).ID(); got != model.NuGet {
		t.Fatalf("ID() = %v", got)
	}
}

func TestNuGetMapsPackagesAndFiltersEmptyDescriptions(t *testing.T) {
	srv := nugetServer(t, 200, nugetBody)
	m := mustSearch(t, nugetFor(srv), query())
	// The empty-description package is filtered out.
	if len(m) != 1 || m[0].Name != "Newtonsoft.Json" || m[0].Source != model.NuGet ||
		m[0].URL != "https://www.nuget.org/packages/Newtonsoft.Json" || deref(m[0].Popularity) != uint64(100) {
		t.Errorf("matches = %+v", m)
	}
	if params := srv.Requests()[0].URL.Query(); params.Get("q") != "async runtime" || params.Get("take") != "20" {
		t.Errorf("params = %v", params)
	}
}

func TestNuGetServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, nugetFor(nugetServer(t, 500, "")), query())
}

func TestNuGetEmptyResultsIsOK(t *testing.T) {
	if m := mustSearch(t, nugetFor(nugetServer(t, 200, `{"data": []}`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestNuGetNarrowsAllTheWayToASingleKeyword(t *testing.T) {
	srv := narrowingServer(t, "/query", "q", narrowingSteps[:3], `{"data": []}`, "mistakes", nugetBody)
	m := mustSearch(t, nugetFor(srv), narrowingQuery())
	if len(m) != 1 || m[0].Name != "Newtonsoft.Json" || !slices.Equal(requested(srv, "q"), narrowingSteps) {
		t.Errorf("matches = %+v after %q", m, requested(srv, "q"))
	}
}

// ── RubyGems ──────────────────────────────────────────────────────────────

const rubygemsBody = `[{"name": "rails", "info": "Full-stack web framework", "project_uri": "https://rubygems.org/gems/rails", "downloads": 999}]`

func rubygemsFor(srv *mockServer) *RubyGems {
	return &RubyGems{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func rubygemsServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/api/v1/search.json", respond(status, body)))
}

func TestRubyGemsIDIsRubyGems(t *testing.T) {
	if got := NewRubyGems(nil).ID(); got != model.RubyGems {
		t.Fatalf("ID() = %v", got)
	}
}

func TestRubyGemsMapsGemsIntoMatches(t *testing.T) {
	srv := rubygemsServer(t, 200, rubygemsBody)
	m := mustSearch(t, rubygemsFor(srv), query())
	if len(m) != 1 || m[0].Name != "rails" || m[0].Source != model.RubyGems || m[0].URL != "https://rubygems.org/gems/rails" ||
		m[0].Description != "Full-stack web framework" || deref(m[0].Popularity) != uint64(999) {
		t.Errorf("matches = %+v", m)
	}
	if got := requested(srv, "query"); !slices.Equal(got, []string{"async runtime"}) {
		t.Errorf("queries = %q", got)
	}
}

func TestRubyGemsServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, rubygemsFor(rubygemsServer(t, 500, "")), query())
}

func TestRubyGemsEmptyResultsIsOK(t *testing.T) {
	if m := mustSearch(t, rubygemsFor(rubygemsServer(t, 200, `[]`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

func TestRubyGemsFiltersEmptyInfoTakes20AndFallsBackToTheGemPage(t *testing.T) {
	var b strings.Builder
	b.WriteString(`[{"name": "no-info", "info": ""}`)
	for i := range 25 {
		b.WriteString(`, {"name": "g` + string(rune('a'+i)) + `", "info": "x"}`)
	}
	b.WriteString(`]`)
	m := mustSearch(t, rubygemsFor(rubygemsServer(t, 200, b.String())), query())
	if len(m) != 20 || m[0].Name != "ga" || m[0].URL != "https://rubygems.org/gems/ga" || m[0].Popularity != nil {
		t.Errorf("got %d matches, first %+v", len(m), m[0])
	}
}

func TestRubyGemsNarrowsWhenTheFullQueryIsEmpty(t *testing.T) {
	srv := narrowingServer(t, "/api/v1/search.json", "query", narrowingSteps[:1], `[]`, "spell syntax mistakes", rubygemsBody)
	m := mustSearch(t, rubygemsFor(srv), narrowingQuery())
	if len(m) != 1 || m[0].Name != "rails" || len(srv.Requests()) != 2 {
		t.Errorf("matches = %+v after %q", m, requested(srv, "query"))
	}
}

// ── Docker Hub ────────────────────────────────────────────────────────────

const dockerBody = `{"results": [
  {"repo_name": "library/nginx", "short_description": "Official nginx image", "star_count": 200},
  {"repo_name": "no/desc", "short_description": "", "star_count": 1}
]}`

func dockerFor(srv *mockServer) *DockerHub {
	return &DockerHub{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func dockerServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/v2/search/repositories/", respond(status, body)))
}

func TestDockerHubIDIsDockerHub(t *testing.T) {
	if got := NewDockerHub(nil).ID(); got != model.DockerHub {
		t.Fatalf("ID() = %v", got)
	}
}

func TestDockerHubMapsReposAndFiltersEmptyDescriptions(t *testing.T) {
	srv := dockerServer(t, 200, dockerBody)
	m := mustSearch(t, dockerFor(srv), query())
	if len(m) != 1 || m[0].Name != "library/nginx" || m[0].Source != model.DockerHub ||
		m[0].URL != "https://hub.docker.com/r/library/nginx" || deref(m[0].Popularity) != uint64(200) {
		t.Errorf("matches = %+v", m)
	}
	if params := srv.Requests()[0].URL.Query(); params.Get("query") != "async runtime" || params.Get("page_size") != "20" {
		t.Errorf("params = %v", params)
	}
}

func TestDockerHubServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, dockerFor(dockerServer(t, 500, "")), query())
}

func TestDockerHubEmptyResultsIsOK(t *testing.T) {
	if m := mustSearch(t, dockerFor(dockerServer(t, 200, `{"results": []}`)), query()); len(m) != 0 {
		t.Errorf("matches = %+v", m)
	}
}

// ── VS Code Marketplace ───────────────────────────────────────────────────

const vscodeBody = `{"results": [{"extensions": [{
  "publisher": {"publisherName": "rust-lang"},
  "extensionName": "rust-analyzer",
  "displayName": "rust-analyzer",
  "shortDescription": "Rust language support",
  "statistics": [{"statisticName": "install", "value": 12345.0}],
  "lastUpdated": "2026-08-27T09:58:55.6+00:00"
}]}]}`

func vscodeFor(srv *mockServer) *VSCodeMarketplace {
	return &VSCodeMarketplace{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func vscodeServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/_apis/public/gallery/extensionquery", respond(status, body)))
}

func TestVSCodeIDIsVSCode(t *testing.T) {
	if got := NewVSCodeMarketplace(nil).ID(); got != model.VSCodeMarketplace {
		t.Fatalf("ID() = %v", got)
	}
}

func TestVSCodeMapsExtensionsIntoMatches(t *testing.T) {
	m := mustSearch(t, vscodeFor(vscodeServer(t, 200, vscodeBody)), query())
	if len(m) != 1 || m[0].Name != "rust-analyzer" || m[0].Source != model.VSCodeMarketplace ||
		m[0].URL != "https://marketplace.visualstudio.com/items?itemName=rust-lang.rust-analyzer" ||
		m[0].Description != "Rust language support" || deref(m[0].Popularity) != uint64(12345) {
		t.Errorf("matches = %+v", m)
	}
}

func TestVSCodeSendsTheGalleryQuery(t *testing.T) {
	var got struct {
		Method, Accept, ContentType string
		Body                        map[string]any
	}
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Method, got.Accept, got.ContentType = r.Method, r.Header.Get("Accept"), r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&got.Body)
		respond(200, vscodeBody)(w, r)
	}))
	mustSearch(t, vscodeFor(srv), query())
	if got.Method != "POST" || got.Accept != "application/json;api-version=7.1-preview.1" || got.ContentType != "application/json" {
		t.Errorf("request = %+v", got)
	}
	want := `{"filters":[{"criteria":[{"filterType":8,"value":"Microsoft.VisualStudio.Code"},{"filterType":10,"value":"async runtime"}],"pageNumber":1,"pageSize":20}],"flags":914}`
	if b, _ := json.Marshal(got.Body); string(b) != want {
		t.Errorf("body = %s\nwant   %s", b, want)
	}
}

func TestVSCodeServerErrorIsPropagated(t *testing.T) {
	wantSearchError(t, vscodeFor(vscodeServer(t, 500, "")), query())
}

func TestVSCodeEmptyResultsIsOK(t *testing.T) {
	for _, body := range []string{`{"results": [{"extensions": []}]}`, `{"results": []}`} {
		if m := mustSearch(t, vscodeFor(vscodeServer(t, 200, body)), query()); len(m) != 0 {
			t.Errorf("%s: matches = %+v", body, m)
		}
	}
}

func TestVSCodeNormalisesASingleDigitFractionAndExplicitOffset(t *testing.T) {
	// The only source seen writing a one-digit fraction and a numeric zero
	// offset rather than Z.
	m := mustSearch(t, vscodeFor(vscodeServer(t, 200, vscodeBody)), query())
	if deref(m[0].LastUpdated) != "2026-08-27T09:58:55Z" {
		t.Errorf("last_updated = %v", deref(m[0].LastUpdated))
	}
}

// The gallery only returns lastUpdated under flags: 914; a narrower flag set
// drops the field entirely, which must read as "no date", not fail.
func TestVSCodeSurvivesAMissingOrBrokenDate(t *testing.T) {
	for _, body := range []string{
		strings.Replace(vscodeBody, `,
  "lastUpdated": "2026-08-27T09:58:55.6+00:00"`, "", 1),
		strings.Replace(vscodeBody, `"2026-08-27T09:58:55.6+00:00"`, "1756288735", 1),
	} {
		if m := mustSearch(t, vscodeFor(vscodeServer(t, 200, body)), query()); len(m) != 1 || m[0].LastUpdated != nil {
			t.Errorf("matches = %+v", m)
		}
	}
}

func TestVSCodeDisplayNameAndInstallsFallBack(t *testing.T) {
	body := `{"results": [{"extensions": [
	  {"publisher": {"publisherName": "p"}, "extensionName": "plain", "shortDescription": "d"},
	  {"publisher": {"publisherName": "p"}, "extensionName": "nodesc", "shortDescription": ""}
	]}, {"extensions": [
	  {"publisher": {"publisherName": "p"}, "extensionName": "second-result", "shortDescription": "d"}
	]}]}`
	m := mustSearch(t, vscodeFor(vscodeServer(t, 200, body)), query())
	// Only the first result counts; no display name means the extension
	// name, and no install statistic means no popularity.
	if len(m) != 1 || m[0].Name != "plain" || m[0].Popularity != nil {
		t.Errorf("matches = %+v", m)
	}
	broken := strings.Replace(body, `"extensionName": "second-result", `, "", 1)
	wantSearchError(t, vscodeFor(vscodeServer(t, 200, broken)), query())
}

func TestSaturatingUint64(t *testing.T) {
	for f, want := range map[float64]uint64{12345.9: 12345, -1: 0, 0: 0, 1e30: 1<<64 - 1} {
		if got := saturatingUint64(f); got != want {
			t.Errorf("saturatingUint64(%v) = %v, want %v", f, got, want)
		}
	}
}
