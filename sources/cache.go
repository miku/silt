package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// put keeps matches as a's results for q. The entry is written to a temporary
// file and renamed into place, so a concurrent run never reads half of it.
func (c *Cache) put(a Adapter, q model.Query, matches []model.Match) {
	if !c.enabled() {
		return
	}
	b, err := json.Marshal(cacheEntry{cacheVersion, a.ID(), q.Idea, q.Keywords, matches})
	if err != nil {
		return
	}
	// Owner only: with a GitHub token, results can name private repositories.
	if os.MkdirAll(c.Dir, 0o700) != nil {
		return
	}
	f, err := os.CreateTemp(c.Dir, "*.tmp")
	if err != nil {
		return
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), c.path(a, q))
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
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
