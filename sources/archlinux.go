package sources

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/model"
)

const (
	archLinuxBaseURL = "https://archlinux.org"
	// archLinuxMaxHits is the most packages one search contributes. A single
	// broad keyword matches hundreds ("process" 262), which the server
	// lists by name, so the ones that hold the most keywords of the idea are
	// kept.
	archLinuxMaxHits = 20
)

// ArchLinux searches the official Arch Linux repositories (core, extra,
// multilib and their testing and staging repositories) through the package
// search of archlinux.org: GET /packages/search/json/.
type ArchLinux struct {
	Client  *http.Client
	BaseURL string
}

// NewArchLinux returns an adapter for the live archlinux.org.
func NewArchLinux(client *http.Client) *ArchLinux {
	return &ArchLinux{Client: client, BaseURL: archLinuxBaseURL}
}

// ID implements Adapter.
func (*ArchLinux) ID() model.Source { return model.ArchLinux }

type archPackage struct {
	PkgName *string `json:"pkgname"`
	Repo    *string `json:"repo"`
	Arch    *string `json:"arch"`
	PkgDesc string  `json:"pkgdesc"`
	// When the package last changed in the repositories, a rebuild
	// included.
	LastUpdate freshness.Lenient[string] `json:"last_update"`
}

// Search implements Adapter.
func (a *ArchLinux) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// Every word of q has to appear in the package's name or description, in
	// any order and case. Measured live, four of six realistic ideas found
	// nothing until they were down to a single keyword ("keybindings" finds
	// a TUI with vim keybindings, "terminal keybindings" nothing), so like
	// the AUR this narrows all the way.
	var matches []model.Match
	for _, term := range narrowingCandidates(q, 1) {
		var body struct {
			Results *[]archPackage `json:"results"`
		}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/packages/search/json/", queryParams{{"q", term}}, &body); err != nil {
			return nil, err
		}
		if body.Results == nil {
			return nil, missingField("results")
		}
		for _, p := range *body.Results {
			switch {
			case p.PkgName == nil:
				return nil, missingField("pkgname")
			case p.Repo == nil:
				return nil, missingField("repo")
			case p.Arch == nil:
				return nil, missingField("arch")
			}
		}
		if matches = a.toMatches(*body.Results, q.Keywords); len(matches) > 0 {
			break
		}
	}
	return matches, nil
}

// archStableRepos are the repositories of released packages; the others
// (core-testing, extra-staging, kde-unstable, …) hold the next builds.
var archStableRepos = []string{"core", "extra", "multilib"}

// toMatches keeps one entry per package, from a stable repository where it
// is in one, and of those the archLinuxMaxHits that hold the most keywords,
// in the server's order otherwise.
func (a *ArchLinux) toMatches(pkgs []archPackage, keywords []string) []model.Match {
	var kept []archPackage
	index := make(map[string]int)
	for _, p := range pkgs {
		i, seen := index[*p.PkgName]
		switch {
		case !seen:
			index[*p.PkgName] = len(kept)
			kept = append(kept, p)
		case !slices.Contains(archStableRepos, *kept[i].Repo) && slices.Contains(archStableRepos, *p.Repo):
			kept[i] = p
		}
	}

	// A keyword counts if a word of the name or description starts with its
	// stem: "kills" counts for "kill processes", but "port" not for
	// "support". Measured live, that lifts stern, the multi-pod log tailer,
	// from third to first of 38 for "kubernetes log viewer with live
	// filtering", where anywhere in the text it trailed two packages that
	// merely mention Kubernetes and one of the other keywords.
	var terms []string
	for _, k := range keywords {
		if t := stemS(textutil.ToLower(k)); t != "" && !slices.Contains(terms, t) {
			terms = append(terms, t)
		}
	}
	notAlnum := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	type scored struct {
		pkg  archPackage
		hits int
	}
	ranked := make([]scored, len(kept))
	for i, p := range kept {
		words := strings.FieldsFunc(textutil.ToLower(*p.PkgName+" "+p.PkgDesc), notAlnum)
		ranked[i].pkg = p
		for _, t := range terms {
			if slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, t) }) {
				ranked[i].hits++
			}
		}
	}
	slices.SortStableFunc(ranked, func(x, y scored) int { return cmp.Compare(y.hits, x.hits) })

	var matches []model.Match
	for _, r := range ranked[:min(archLinuxMaxHits, len(ranked))] {
		p := r.pkg
		matches = append(matches, model.Match{
			Name:        *p.PkgName,
			Source:      model.ArchLinux,
			URL:         a.BaseURL + "/packages/" + *p.Repo + "/" + *p.Arch + "/" + *p.PkgName + "/",
			Description: p.PkgDesc,
			LastUpdated: rfc3339Date(p.LastUpdate),
		})
	}
	return matches
}
