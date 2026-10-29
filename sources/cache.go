package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/miku/silt/model"
)

// DefaultCacheTTL is how long a search result stays in the cache unless the
// caller says otherwise: long enough that refining a verdict or rerunning an
// idea doesn't search the registries again, short enough that the results
// stay current.
const DefaultCacheTTL = time.Hour

// cacheVersion is the format of a cache entry. Entries of another version are
// misses.
const cacheVersion = 1

// Cache keeps the results of successful searches on disk, one file per source
// and query, so that a repeated search within TTL is answered locally instead
// of being sent to the registry again.
//
// Only successes are kept. A source that failed is searched again next time,
// so a passing outage never turns into an hour of "not reached", and a
// cached result is always one the source actually returned. A genuinely empty
// result is a success and is kept.
//
// The cache is best effort: an entry that can't be read is a miss, and one
// that can't be written is skipped. A nil *Cache, or one with a TTL of zero or
// less, caches nothing.
type Cache struct {
	// Dir holds the entries; it is created on the first write.
	Dir string
	// TTL is how long an entry is used after it was written.
	TTL time.Duration
}

// DefaultCacheDir is where silt keeps search results:
// <UserCacheDir>/silt/results, next to the embedding model.
func DefaultCacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "silt", "results"), nil
}

// cacheKeyer is implemented by adapters whose results depend on more than the
// query, such as GitHub's: a token widens the search to the private
// repositories it can see.
type cacheKeyer interface {
	cacheKey() string
}

// cacheEntry is one cached search result, as stored on disk.
type cacheEntry struct {
	Version  int           `json:"version"`
	Source   model.Source  `json:"source"`
	Idea     string        `json:"idea"`
	Keywords []string      `json:"keywords"`
	Matches  []model.Match `json:"matches"`
}

func (c *Cache) enabled() bool { return c != nil && c.TTL > 0 }

// path returns the file of a's results for q.
func (c *Cache) path(a Adapter, q model.Query) string {
	key := struct {
		Version  int
		Source   string
		Idea     string
		Keywords []string
		Extra    string
	}{cacheVersion, a.ID().KebabName(), q.Idea, q.Keywords, ""}
	if k, ok := a.(cacheKeyer); ok {
		key.Extra = k.cacheKey()
	}
	b, _ := json.Marshal(key) // strings and ints only: cannot fail
	sum := sha256.Sum256(b)
	return filepath.Join(c.Dir, a.ID().KebabName()+"-"+hex.EncodeToString(sum[:16])+".json")
}

// get returns a's cached results for q, if there is an entry younger than
// TTL.
func (c *Cache) get(a Adapter, q model.Query) ([]model.Match, bool) {
	if !c.enabled() {
		return nil, false
	}
	path := c.path(a, q)
	fi, err := os.Stat(path)
	if err != nil || time.Since(fi.ModTime()) >= c.TTL {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var e cacheEntry
	if json.Unmarshal(b, &e) != nil || e.Version != cacheVersion || e.Source != a.ID() ||
		e.Idea != q.Idea || !slices.Equal(e.Keywords, q.Keywords) {
		return nil, false
	}
	return e.Matches, true
}

// put keeps matches as a's results for q.
func (c *Cache) put(a Adapter, q model.Query, matches []model.Match) {
	if !c.enabled() {
		return
	}
	b, err := json.Marshal(cacheEntry{cacheVersion, a.ID(), q.Idea, q.Keywords, matches})
	if err != nil {
		return
	}
	_ = writeAtomic(c.path(a, q), b)
}

// writeAtomic writes b to path through a temporary file renamed into place,
// so that a concurrent run never reads half of it. The directory is created
// if need be, readable by its owner only: with a GitHub token, results can
// name private repositories.
func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

// prune removes the entries older than TTL, and temporary files a crashed
// run left behind, so that the directory doesn't grow without bound.
func (c *Cache) prune() {
	if !c.enabled() {
		return
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) >= c.TTL {
			_ = os.Remove(filepath.Join(c.Dir, name))
		}
	}
}

// DefaultCatalogTTL is how long a downloaded catalog is used unless the
// caller says otherwise. Homebrew's change every few minutes, but a day-old
// catalog is as good for finding prior art, and spares downloading 51 MB on
// every run.
const DefaultCatalogTTL = 24 * time.Hour

// CatalogCache keeps on disk the catalogs that a source downloads whole and
// searches locally, as Homebrew's adapter does, so that a run within TTL of
// the download doesn't download them again. Unlike [Cache], which keeps one
// search's results, a catalog answers any query.
//
// Like Cache it is best effort, keeps only successful downloads, and caches
// nothing when nil or when its TTL is zero or less.
type CatalogCache struct {
	// Dir holds the catalogs; it is created on the first write.
	Dir string
	// TTL is how long a catalog is used after it was downloaded.
	TTL time.Duration
}

// DefaultCatalogDir is where silt keeps catalogs: <UserCacheDir>/silt/catalogs.
// It is not the results directory, whose entries are pruned after an hour.
func DefaultCatalogDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "silt", "catalogs"), nil
}

func (c *CatalogCache) enabled() bool { return c != nil && c.TTL > 0 }

// load decodes the catalog called name into v, if there is one younger than
// TTL that decodes.
func (c *CatalogCache) load(name string, v any) bool {
	if !c.enabled() {
		return false
	}
	path := filepath.Join(c.Dir, name+".json")
	fi, err := os.Stat(path)
	if err != nil || time.Since(fi.ModTime()) >= c.TTL {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

// store keeps v as the catalog called name.
func (c *CatalogCache) store(name string, v any) {
	if !c.enabled() {
		return
	}
	if b, err := json.Marshal(v); err == nil {
		_ = writeAtomic(filepath.Join(c.Dir, name+".json"), b)
	}
}

// memCatalog keeps a catalog in memory for the rest of the process. It is
// fetched at most once successfully; a failed fetch is tried again by the
// next search.
type memCatalog[T any] struct {
	mu      sync.Mutex
	v       T
	fetched bool
}

func (c *memCatalog[T]) get(ctx context.Context, fetch func(context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetched {
		v, err := fetch(ctx)
		if err != nil {
			return v, err
		}
		c.v, c.fetched = v, true
	}
	return c.v, nil
}
