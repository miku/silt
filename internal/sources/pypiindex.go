package sources

import (
	"cmp"
	"context"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/miku/silt/internal/freshness"
	"github.com/miku/silt/internal/model"
)

// PyPI's search is walled, but its index of project names (the simple
// index, which pip reads) and its JSON API are not. When no browser gets
// through, the PyPI adapter matches the keywords against every project's
// name, and fetches the description of the best matches from the JSON API.
// It finds less than PyPI's search, which also reads descriptions and knows
// what is popular, but it reaches PyPI.

const (
	// maxPyPIIndexResults is how many of the best-matching names are
	// described and returned.
	maxPyPIIndexResults = 20
	// maxPyPIDescriptions is how many descriptions are fetched at once.
	maxPyPIDescriptions = 4
	// pypiIndexVersion is the format of the index on disk. An index of
	// another version is a miss.
	pypiIndexVersion = 1
)

// sharedPyPIIndex is the index every live adapter uses, so that a session
// that searches several times loads it once.
var sharedPyPIIndex = &memCatalog[[]string]{}

// pypiGenericWords are keywords that say nothing about what a PyPI project
// is for: every project is Python, and most are a library, a package or a
// tool. Matched against names, they would rank "python-library" first.
var pypiGenericWords = func() map[string]bool {
	words := map[string]bool{}
	for _, w := range []string{"python", "py", "pypi", "library", "libraries", "lib", "package", "module", "tool", "util", "utility", "utilities"} {
		words[stemS(w)] = true
	}
	return words
}()

// pypiIndexFile is PyPI's index on disk: the names of all projects, in its
// order.
type pypiIndexFile struct {
	Version int      `json:"version"`
	BaseURL string   `json:"base_url"`
	Names   []string `json:"names"`
}

// searchIndex matches q's keywords against the names of all projects, and
// describes the best matches.
func (a *PyPI) searchIndex(ctx context.Context, q model.Query) ([]model.Match, error) {
	a.init.Do(func() {
		if a.index == nil {
			a.index = &memCatalog[[]string]{}
		}
	})
	names, err := a.index.get(ctx, a.loadIndex)
	if err != nil {
		return nil, err
	}
	return a.describe(ctx, matchPyPINames(names, q.Keywords, maxPyPIIndexResults)), nil
}

// loadIndex returns the names of all projects: from disk, if a fresh index
// from the same host is kept there, or else downloaded from the simple index
// as JSON (PEP 691), and kept. In October 2026 that was 926,592 names, 10 MB
// compressed.
func (a *PyPI) loadIndex(ctx context.Context) ([]string, error) {
	var file pypiIndexFile
	if a.Catalogs.load("pypi-names", &file) && file.Version == pypiIndexVersion && file.BaseURL == a.BaseURL {
		return file.Names, nil
	}
	req, err := newRequest(ctx, http.MethodGet, a.BaseURL+"/simple/", nil, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.pypi.simple.v1+json")
	resp, err := send(a.Client, req)
	if err != nil {
		return nil, err
	}
	var index struct {
		Projects *[]struct {
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := decodeJSON(resp, &index); err != nil {
		return nil, err
	}
	if index.Projects == nil {
		return nil, missingField("projects")
	}
	names := make([]string, 0, len(*index.Projects))
	for _, p := range *index.Projects {
		if p.Name != "" {
			names = append(names, p.Name)
		}
	}
	// An index without a single name has changed shape, and would leave
	// every search empty for a day.
	if len(names) == 0 {
		return nil, missingField("name")
	}
	a.Catalogs.store("pypi-names", pypiIndexFile{Version: pypiIndexVersion, BaseURL: a.BaseURL, Names: names})
	return names, nil
}

// matchPyPINames returns the names that best match keywords, at most limit,
// best first.
//
// A name is split into parts at "-", "_" and ".". A keyword matches a part
// that is the keyword, with or without a final "s", or, if the keyword has
// four letters or more, a part that starts with it: "kill" matches
// "port_killer". Each keyword a name matches counts by how rare the keyword
// is among the names (its inverse document frequency), so that "toml" counts
// for more than "config"; each part that matches no keyword counts one
// against it. Ties go to the shorter name, then to the name in order.
// Generic words (pypiGenericWords) are no keywords here.
func matchPyPINames(names, keywords []string, limit int) []string {
	var terms []string
	for _, k := range keywords {
		k = stemS(strings.ToLower(k))
		if k != "" && !pypiGenericWords[k] && !slices.Contains(terms, k) && len(terms) < 64 {
			terms = append(terms, k)
		}
	}
	if len(terms) == 0 {
		return nil
	}
	type candidate struct {
		name  string
		hits  uint64 // bit i: terms[i] matched
		parts int
		score float64
	}
	var candidates []candidate
	docFreq := make([]int, len(terms))
	isSep := func(r rune) bool { return r == '-' || r == '_' || r == '.' }
	for _, name := range names {
		var hits uint64
		parts := 0
		for part := range strings.FieldsFuncSeq(strings.ToLower(name), isSep) {
			parts++
			for i, t := range terms {
				if part == t || stemS(part) == t || len(t) >= 4 && strings.HasPrefix(part, t) {
					hits |= 1 << i
				}
			}
		}
		if hits == 0 {
			continue
		}
		for i := range terms {
			if hits&(1<<i) != 0 {
				docFreq[i]++
			}
		}
		candidates = append(candidates, candidate{name: name, hits: hits, parts: parts})
	}
	for i := range candidates {
		c := &candidates[i]
		matched := 0
		for j := range terms {
			if c.hits&(1<<j) != 0 {
				// Not below 0, which a keyword in every name would be.
				c.score += max(math.Log(float64(len(names))/float64(1+docFreq[j])), 0)
				matched++
			}
		}
		c.score -= float64(max(c.parts-matched, 0))
	}
	slices.SortFunc(candidates, func(x, y candidate) int {
		return cmp.Or(cmp.Compare(y.score, x.score), cmp.Compare(len(x.name), len(y.name)), strings.Compare(x.name, y.name))
	})
	best := make([]string, 0, min(limit, len(candidates)))
	for _, c := range candidates[:min(limit, len(candidates))] {
		best = append(best, c.name)
	}
	return best
}

// stemS removes a final "s" from a word of four letters or more: "files" is
// "file", "less" is "les", "css" stays.
func stemS(w string) string {
	if len(w) > 3 && w[len(w)-1] == 's' {
		return w[:len(w)-1]
	}
	return w
}

// describe returns names as matches, with the summary and the upload date of
// the latest release from PyPI's JSON API, maxPyPIDescriptions at a time, in
// the order of names. A project that no longer exists (the index on disk can
// be a day old) is left out; one whose description can't be fetched is kept
// without it.
func (a *PyPI) describe(ctx context.Context, names []string) []model.Match {
	matches := make([]model.Match, len(names))
	exists := make([]bool, len(names))
	var wg sync.WaitGroup
	fetching := make(chan struct{}, maxPyPIDescriptions)
	for i, name := range names {
		wg.Go(func() {
			fetching <- struct{}{}
			defer func() { <-fetching }()
			matches[i], exists[i] = a.describeOne(ctx, name)
		})
	}
	wg.Wait()
	var out []model.Match
	for i, m := range matches {
		if exists[i] {
			out = append(out, m)
		}
	}
	return out
}

// describeOne returns the match for the project called name, and whether the
// project exists.
func (a *PyPI) describeOne(ctx context.Context, name string) (model.Match, bool) {
	m := model.Match{Name: name, Source: model.PyPI, URL: a.BaseURL + "/project/" + name + "/"}
	resp, err := get(ctx, a.Client, a.BaseURL+"/pypi/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return m, true
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return m, false
	}
	var body struct {
		Info struct {
			Summary string `json:"summary"`
		} `json:"info"`
		// URLs are the files of the latest release.
		URLs []struct {
			UploadTime freshness.Lenient[string] `json:"upload_time_iso_8601"`
		} `json:"urls"`
	}
	if decodeJSON(resp, &body) != nil {
		return m, true
	}
	m.Description = strings.TrimSpace(body.Info.Summary)
	for _, u := range body.URLs {
		// Canonical RFC 3339 in UTC: the later date sorts later.
		if d := rfc3339Date(u.UploadTime); d != nil && (m.LastUpdated == nil || *d > *m.LastUpdated) {
			m.LastUpdated = d
		}
	}
	return m, true
}
