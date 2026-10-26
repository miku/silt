package sources

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/model"
)

const homebrewBaseURL = "https://formulae.brew.sh"

// Homebrew searches Homebrew formulae and casks. There is no search API, so
// it downloads the full catalogs (/api/formula.json and /api/cask.json) once
// and searches them locally.
type Homebrew struct {
	Client  *http.Client
	BaseURL string

	// catalog is the shared, process-wide cache for adapters from
	// NewHomebrew; an adapter built otherwise caches on its own.
	catalog *brewCatalog
	init    sync.Once
}

// sharedBrewCatalog is the catalog every live adapter uses, so that a
// session that searches several times downloads it once.
var sharedBrewCatalog = &brewCatalog{}

// NewHomebrew returns an adapter for the live Homebrew API, which shares its
// catalog with every other adapter NewHomebrew returns.
func NewHomebrew(client *http.Client) *Homebrew {
	return &Homebrew{Client: client, BaseURL: homebrewBaseURL, catalog: sharedBrewCatalog}
}

// ID implements Adapter.
func (*Homebrew) ID() model.Source { return model.Homebrew }

type brewPackage struct {
	name, desc, homepage string
	// lowered for matching
	lowerName, lowerDesc string
}

// brewCatalog is fetched at most once successfully; a failed fetch is tried
// again by the next search.
type brewCatalog struct {
	mu       sync.Mutex
	packages []brewPackage
	fetched  bool
}

func (c *brewCatalog) get(ctx context.Context, fetch func(context.Context) ([]brewPackage, error)) ([]brewPackage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetched {
		pkgs, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		c.packages, c.fetched = pkgs, true
	}
	return c.packages, nil
}

func (a *Homebrew) fetchCatalog(ctx context.Context) ([]brewPackage, error) {
	var formulae []struct {
		Name     *string `json:"name"`
		Desc     string  `json:"desc"`
		Homepage string  `json:"homepage"`
	}
	if err := a.getCatalog(ctx, "/api/formula.json", &formulae); err != nil {
		return nil, err
	}
	if formulae == nil {
		return nil, missingField("formulae") // a JSON null, not a list
	}
	var casks []struct {
		Token    *string `json:"token"`
		Desc     string  `json:"desc"`
		Homepage string  `json:"homepage"`
	}
	if err := a.getCatalog(ctx, "/api/cask.json", &casks); err != nil {
		return nil, err
	}
	if casks == nil {
		return nil, missingField("casks")
	}
	pkgs := make([]brewPackage, 0, len(formulae)+len(casks))
	add := func(name, desc, homepage string) {
		pkgs = append(pkgs, brewPackage{
			name: name, desc: desc, homepage: homepage,
			lowerName: textutil.ToLower(name), lowerDesc: textutil.ToLower(desc),
		})
	}
	for _, f := range formulae {
		if f.Name == nil {
			return nil, missingField("name")
		}
		add(*f.Name, f.Desc, f.Homepage)
	}
	for _, c := range casks {
		if c.Token == nil {
			return nil, missingField("token")
		}
		add(*c.Token, c.Desc, c.Homepage)
	}
	return pkgs, nil
}

func (a *Homebrew) getCatalog(ctx context.Context, path string, v any) error {
	req, err := newRequest(ctx, http.MethodGet, a.BaseURL+path, nil, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := send(a.Client, req)
	if err != nil {
		return err
	}
	return decodeJSON(resp, v)
}

// Search implements Adapter.
func (a *Homebrew) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	a.init.Do(func() {
		if a.catalog == nil {
			a.catalog = &brewCatalog{}
		}
	})
	pkgs, err := a.catalog.get(ctx, a.fetchCatalog)
	if err != nil {
		return nil, err
	}
	// Every keyword has to appear in the formula's name or one-line
	// description, and those descriptions are short: a realistic 7-term idea
	// matches nothing at all, while its 2 longest terms match real formulae.
	// Narrow progressively until something matches. The catalog is already
	// in memory, so the extra passes cost no requests.
	var matches []model.Match
	for _, candidate := range narrowingCandidates(q, 2) {
		terms := strings.Fields(textutil.ToLower(candidate))
		matches = matches[:0]
		for _, p := range pkgs {
			if len(matches) == 20 {
				break
			}
			if !containsAll(p, terms) {
				continue
			}
			url := p.homepage
			if url == "" {
				url = a.BaseURL + "/formula/" + p.name
			}
			matches = append(matches, model.Match{Name: p.name, Source: model.Homebrew, URL: url, Description: p.desc})
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}

// containsAll reports whether every term is in p's name or description.
func containsAll(p brewPackage, terms []string) bool {
	for _, t := range terms {
		if !strings.Contains(p.lowerName, t) && !strings.Contains(p.lowerDesc, t) {
			return false
		}
	}
	return true
}
