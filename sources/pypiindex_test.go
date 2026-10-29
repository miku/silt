package sources

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
)

func TestMatchPyPINamesMatchesNameParts(t *testing.T) {
	names := []string{
		"kill-port",
		"Kill_Ports",          // case, "_" and a plural
		"port.killer",         // "." and a prefix: "kill" starts "killer"
		"portal",              // "port" starts "portal"
		"import-export",       // "port" inside a part is no match
		"python-library-tool", // generic words only
		"clickable",           // "cli" is too short to match as a prefix
		"cli",
		"clis",
		"unrelated",
	}
	got := matchPyPINames(names, []string{"python", "kill", "port", "cli", "library"}, 20)
	slices.Sort(got)
	want := []string{"Kill_Ports", "cli", "clis", "kill-port", "port.killer", "portal"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("matched %q, want %q", got, want)
	}
	if got := matchPyPINames(names, []string{"python", "library"}, 20); got != nil {
		t.Errorf("generic keywords only: %q, want nothing", got)
	}
	if got := matchPyPINames(names, nil, 20); got != nil {
		t.Errorf("no keywords: %q, want nothing", got)
	}
}

func TestMatchPyPINamesRanking(t *testing.T) {
	for _, tt := range []struct {
		name     string
		names    []string
		keywords []string
		want     []string
	}{
		{
			"a rarer keyword counts for more",
			[]string{"config", "toml", "config-x", "x-config", "unrelated"},
			[]string{"config", "toml"},
			[]string{"toml", "config", "config-x", "x-config"},
		},
		{
			"two keywords beat one",
			[]string{"toml", "config-toml", "unrelated", "other", "more"},
			[]string{"toml", "config"},
			[]string{"config-toml", "toml"},
		},
		{
			"each part that matches nothing counts against",
			[]string{"toml-a-b", "toml-a", "toml", "x", "y", "z"},
			[]string{"toml"},
			[]string{"toml", "toml-a", "toml-a-b"},
		},
		{
			"ties go to the shorter name, then in order",
			[]string{"toml-bb", "toml-b", "toml-a", "x", "y", "z"},
			[]string{"toml"},
			[]string{"toml-a", "toml-b", "toml-bb"},
		},
	} {
		if got := matchPyPINames(tt.names, tt.keywords, 20); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	if got := matchPyPINames([]string{"toml-a", "toml-b", "toml-c"}, []string{"toml"}, 2); len(got) != 2 {
		t.Errorf("limit 2: %q", got)
	}
}

// pypiIndexServer serves PyPI behind its wall: the search is the challenge,
// /simple/ lists names, and /pypi/<name>/json serves projects, a 404 for any
// other name. It records the Accept header of /simple/.
func pypiIndexServer(t *testing.T, names []string, projects map[string]string) (*mockServer, *[]string) {
	var mu sync.Mutex
	var accepts []string
	index := struct {
		Meta     map[string]string `json:"meta"`
		Projects []map[string]any  `json:"projects"`
	}{Meta: map[string]string{"api-version": "1.4"}}
	for i, n := range names {
		index.Projects = append(index.Projects, map[string]any{"name": n, "_last-serial": i})
	}
	body, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/":
			respond(200, pypiChallenge)(w, r)
		case r.URL.Path == "/simple/":
			mu.Lock()
			accepts = append(accepts, r.Header.Get("Accept"))
			mu.Unlock()
			respond(200, string(body))(w, r)
		case strings.HasPrefix(r.URL.Path, "/pypi/") && strings.HasSuffix(r.URL.Path, "/json"):
			p, ok := projects[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pypi/"), "/json")]
			switch {
			case !ok:
				respond(404, `{"message": "Not Found"}`)(w, r)
			case p == "500":
				respond(500, "")(w, r)
			default:
				respond(200, p)(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, &accepts
}

// project is a /pypi/<name>/json body; releases, which PyPI also sends and
// silt doesn't read, can be large.
func project(summary string, uploads ...string) string {
	var files []string
	for _, u := range uploads {
		files = append(files, `{"filename": "x.whl", "upload_time_iso_8601": "`+u+`"}`)
	}
	return `{"info": {"summary": ` + summary + `, "version": "1.0"}, "urls": [` + strings.Join(files, ",") +
		`], "releases": {"0.1": [{"filename": "old.tar.gz"}]}}`
}

func requestsTo(srv *mockServer, path string) int {
	n := 0
	for _, r := range srv.Requests() {
		if r.URL.Path == path {
			n++
		}
	}
	return n
}

var killPortQuery = model.Query{Idea: "kill whatever process is on a port", Keywords: []string{"kill", "whatever", "process", "port"}}

func TestPyPIFallsBackToTheIndexWhenWalled(t *testing.T) {
	srv, accepts := pypiIndexServer(t,
		[]string{"unrelated", "kill-process-tree", "port_killer", "python-library"},
		map[string]string{
			"port_killer":       project(`"Kill whatever listens on a port"`, "2025-01-02T10:00:00.123456Z", "2025-03-04T09:30:00.5Z"),
			"kill-process-tree": project(`" Kill a process and its children "`),
		})
	a := pypiFor(srv)
	a.Index = true
	m, how, err := a.searchDetour(t.Context(), killPortQuery)
	if err != nil || how != byName {
		t.Fatalf("Search = %+v, detour %v, %v; want the matches by name", m, how, err)
	}
	updated := "2025-03-04T09:30:00Z"
	want := []model.Match{
		{Name: "port_killer", Source: model.PyPI, URL: srv.URL + "/project/port_killer/", Description: "Kill whatever listens on a port", LastUpdated: &updated},
		{Name: "kill-process-tree", Source: model.PyPI, URL: srv.URL + "/project/kill-process-tree/", Description: "Kill a process and its children"},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("matches:\n got %+v\nwant %+v", m, want)
	}
	if want := []string{"application/vnd.pypi.simple.v1+json"}; !reflect.DeepEqual(*accepts, want) {
		t.Errorf("/simple/ Accept %q, want %q", *accepts, want)
	}
	if n := requestsTo(srv, "/pypi/unrelated/json") + requestsTo(srv, "/pypi/python-library/json"); n != 0 {
		t.Errorf("described %d names that don't match", n)
	}
}

func TestPyPIIndexDropsVanishedProjectsAndKeepsUndescribedOnes(t *testing.T) {
	srv, _ := pypiIndexServer(t, []string{"kill-port", "kill-proc", "kill-all"}, map[string]string{
		"kill-proc": "500",
		"kill-all":  project("null"),
	})
	a := pypiFor(srv)
	a.Index = true
	m, _, err := a.searchDetour(t.Context(), killPortQuery)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range m {
		names = append(names, x.Name)
		if x.Description != "" {
			t.Errorf("%s has description %q", x.Name, x.Description)
		}
	}
	slices.Sort(names)
	if want := []string{"kill-all", "kill-proc"}; !reflect.DeepEqual(names, want) {
		t.Errorf("matches %q, want %q: kill-port no longer exists", names, want)
	}
}

func TestPyPIIndexIsKeptOnDiskAndInMemory(t *testing.T) {
	srv, _ := pypiIndexServer(t, []string{"kill-port"}, map[string]string{"kill-port": project(`"Kill a port"`)})
	c := testCatalogs(t)
	run := func(a *PyPI) {
		t.Helper()
		if _, how, err := a.searchDetour(t.Context(), killPortQuery); err != nil || how != byName {
			t.Fatalf("detour %v, %v", how, err)
		}
	}
	first := pypiFor(srv)
	first.Index, first.Catalogs = true, c
	run(first)
	run(first) // the same process: from memory
	second := pypiFor(srv)
	second.Index, second.Catalogs = true, c
	run(second) // a new process: from disk
	if n := requestsTo(srv, "/simple/"); n != 1 {
		t.Errorf("downloaded the index %d times, want once", n)
	}
	b, err := os.ReadFile(filepath.Join(c.Dir, "pypi-names.json"))
	if err != nil || !strings.Contains(string(b), `"names":["kill-port"]`) {
		t.Errorf("index on disk: %s, %v", b, err)
	}
}

func TestPyPIUsesTheIndexOnlyWhenNoBrowserGotThrough(t *testing.T) {
	srv, _ := pypiIndexServer(t, []string{"kill-port"}, map[string]string{"kill-port": project(`"Kill a port"`)})
	search := func(b Browser, index bool) ([]model.Match, detour, error) {
		a := pypiFor(srv)
		a.Browser, a.Index = b, index
		return a.searchDetour(t.Context(), killPortQuery)
	}
	if _, how, err := search(&fakeBrowser{dom: pypiHTML}, true); err != nil || how != throughBrowser || requestsTo(srv, "/simple/") != 0 {
		t.Errorf("a browser that got through: detour %v, %v, %d index downloads", how, err, requestsTo(srv, "/simple/"))
	}
	if m, how, err := search(&fakeBrowser{dom: pypiChallenge}, true); err != nil || how != byName || len(m) != 1 {
		t.Errorf("a browser that was walled: %+v, detour %v, %v; want the index", m, how, err)
	}
	_, how, err := search(nil, false)
	var unavailable *silt.UnavailableError
	if !errors.As(err, &unavailable) || how != noDetour {
		t.Errorf("neither: detour %v, %v; want PyPI unavailable", how, err)
	}
}

func TestAnIndexThatCantBeLoadedLeavesPyPIUnavailable(t *testing.T) {
	for name, body := range map[string]string{
		"server error":  "500",
		"no projects":   `{"meta": {}}`,
		"no names":      `{"projects": [{"_last-serial": 1}]}`,
		"not json":      `<html>`,
		"empty listing": `{"projects": []}`,
	} {
		srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/search/":
				respond(200, pypiChallenge)(w, r)
			case "/simple/":
				if body == "500" {
					respond(500, "")(w, r)
					return
				}
				respond(200, body)(w, r)
			}
		}))
		c := testCatalogs(t)
		a := pypiFor(srv)
		a.Index, a.Catalogs = true, c
		_, how, err := a.searchDetour(t.Context(), killPortQuery)
		var unavailable *silt.UnavailableError
		if !errors.As(err, &unavailable) || how != noDetour || !strings.Contains(err.Error(), "its index of names") {
			t.Errorf("%s: detour %v, %v", name, how, err)
		}
		if _, err := os.Stat(c.Dir); err == nil {
			t.Errorf("%s: an index was kept", name)
		}
	}
}

func TestPyPIDescribesFewProjectsAtOnce(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var names []string
	for i := range 12 {
		names = append(names, "kill-port-"+string(rune('a'+i)))
	}
	index, _ := json.Marshal(map[string]any{"projects": func() (ps []map[string]string) {
		for _, n := range names {
			ps = append(ps, map[string]string{"name": n})
		}
		return
	}()})
	srv := newMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/":
			respond(200, pypiChallenge)(w, r)
		case "/simple/":
			respond(200, string(index))(w, r)
		default:
			mu.Lock()
			inFlight++
			peak = max(peak, inFlight)
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			respond(200, project(`"x"`))(w, r)
		}
	}))
	a := pypiFor(srv)
	a.Index = true
	if m, _, err := a.searchDetour(t.Context(), killPortQuery); err != nil || len(m) != 12 {
		t.Fatalf("%d matches, %v", len(m), err)
	}
	// 4, not maxPyPIDescriptions: the test is of the cap, not of the constant.
	if peak > 4 || peak < 2 {
		t.Errorf("peak of %d descriptions fetched at once; want at most 4, and more than one", peak)
	}
}

func TestSearcherGivesPyPITheIndexSetting(t *testing.T) {
	c, b := testCatalogs(t), &fakeBrowser{}
	a := (Searcher{PyPIIndex: true, Catalogs: c, Browser: b}).newAdapter(model.PyPI, nil).(*PyPI)
	if !a.Index || a.Catalogs != c || a.Browser != b || a.index != sharedPyPIIndex {
		t.Errorf("adapter %+v", a)
	}
	if a := (Searcher{}).newAdapter(model.PyPI, nil).(*PyPI); a.Index {
		t.Error("the zero Searcher should leave the index off, as patent has none")
	}
}

func TestFanOutReportsAMatchByName(t *testing.T) {
	srv, _ := pypiIndexServer(t, []string{"kill-port"}, map[string]string{"kill-port": project(`"Kill a port"`)})
	a := pypiFor(srv)
	a.Index = true
	out := defaultFanout.search(t.Context(), []Adapter{&staticSource{id: model.CratesIO}, a}, killPortQuery)
	if want := []model.Source{model.PyPI}; !reflect.DeepEqual(out.ByName, want) || len(out.Browsed) != 0 || len(out.Reached) != 2 {
		t.Errorf("by name %v, browsed %v, reached %v", out.ByName, out.Browsed, out.Reached)
	}
}
