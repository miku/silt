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
	// Catalogs, if not nil, keeps the downloaded catalogs on disk, so that
	// the runs within its TTL search them without downloading them.
	Catalogs *CatalogCache

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

// brewCatalogVersion is the format of the catalog on disk. A catalog of
// another version is a miss.
const brewCatalogVersion = 1

// brewCatalogFile is the catalog on disk: only the fields the search reads,
// in the order downloaded. That is under 2 MB of the 51 MB downloaded.
type brewCatalogFile struct {
	Version int    `json:"version"`
	BaseURL string `json:"base_url"`
	// Packages are the formulae, then the casks.
	Packages []brewCatalogEntry `json:"packages"`
}

type brewCatalogEntry struct {
	Name     string `json:"name"`
	Desc     string `json:"desc,omitempty"`
	Homepage string `json:"homepage,omitempty"`
}

// loadCatalog returns the catalog kept on disk, if it is fresh and from the
// same host, or else downloads it and keeps it.
func (a *Homebrew) loadCatalog(ctx context.Context) ([]brewPackage, error) {
	var file brewCatalogFile
	if a.Catalogs.load("homebrew", &file) && file.Version == brewCatalogVersion && file.BaseURL == a.BaseURL {
		pkgs := make([]brewPackage, len(file.Packages))
		for i, e := range file.Packages {
			pkgs[i] = newBrewPackage(e.Name, e.Desc, e.Homepage)
		}
		return pkgs, nil
	}
	pkgs, err := a.fetchCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if a.Catalogs.enabled() {
		file = brewCatalogFile{Version: brewCatalogVersion, BaseURL: a.BaseURL, Packages: make([]brewCatalogEntry, len(pkgs))}
		for i, p := range pkgs {
			file.Packages[i] = brewCatalogEntry{p.name, p.desc, p.homepage}
		}
		a.Catalogs.store("homebrew", file)
	}
	return pkgs, nil
}

func newBrewPackage(name, desc, homepage string) brewPackage {
	return brewPackage{
		name: name, desc: desc, homepage: homepage,
		lowerName: textutil.ToLower(name), lowerDesc: textutil.ToLower(desc),
	}
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
		pkgs = append(pkgs, newBrewPackage(name, desc, homepage))
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
	pkgs, err := a.catalog.get(ctx, a.loadCatalog)
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
