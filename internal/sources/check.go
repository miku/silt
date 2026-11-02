package sources

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/silt"
)

// CheckQuery is what a check searches for: one keyword that every source
// has packages for.
var CheckQuery = model.Query{Idea: "json", Keywords: []string{"json"}}

// A CheckStatus is how a source answered a check.
type CheckStatus int

const (
	// CheckReached: the source's search answered.
	CheckReached CheckStatus = iota
	// CheckBrowsed: the search is walled; the browser got through.
	CheckBrowsed
	// CheckByName: the search is walled; the names in the source's index
	// were matched instead.
	CheckByName
	// CheckWalled: the search is walled, and nothing got past the wall.
	CheckWalled
	// CheckRateLimited: the source refused the search because silt was over
	// its rate limit.
	CheckRateLimited
	// CheckTimedOut: the source didn't answer within the time each source
	// gets.
	CheckTimedOut
	// CheckFailed: any other failure.
	CheckFailed
)

// A SourceCheck is how one source answered a check.
type SourceCheck struct {
	Source model.Source
	Status CheckStatus
	// Elapsed is how long the search took, a retry included.
	Elapsed time.Duration
	// Matches is how many matches the source returned, if it was reached.
	Matches int
	// RateLimit is the source's rate limit, if it refused the search.
	RateLimit RateLimit
	// Err is why the source was not reached, if it wasn't.
	Err error
}

// Reached reports whether the source was reached, one way or another.
func (c SourceCheck) Reached() bool { return c.Status <= CheckByName }

// Check searches each source in include and s.Add once for q, without
// exclude, all at once, and reports how each answered, in [model.All] order.
// A nil include checks every source.
//
// It searches as [Searcher.SearchFiltered] does, with the same retry, the
// same browser and PyPI index, but without the caches, so that every source
// is asked: Homebrew's catalogs, and PyPI's index if PyPI needs it, are
// downloaded.
func (s Searcher) Check(ctx context.Context, q model.Query, include, exclude []model.Source) []SourceCheck {
	if include == nil {
		include = model.All()
	}
	client := s.Client
	if client == nil {
		client = NewHTTPClient()
	}
	s.Cache, s.Catalogs = nil, nil
	ids := selectSources(q.Idea, include, s.Add, exclude)
	checks := make([]SourceCheck, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			a := s.newAdapter(id, client)
			// Nor the catalogs this process already holds.
			switch a := a.(type) {
			case *Homebrew:
				a.catalog = &memCatalog[[]brewPackage]{}
			case *PyPI:
				a.index = &memCatalog[[]string]{}
			}
			start := time.Now()
			r := defaultFanout.searchLive(ctx, a, q)
			checks[i] = newSourceCheck(id, r, time.Since(start))
		})
	}
	wg.Wait()
	return checks
}

// newSourceCheck classifies r, the result of searching id.
func newSourceCheck(id model.Source, r result, elapsed time.Duration) SourceCheck {
	c := SourceCheck{Source: id, Elapsed: elapsed, Err: r.err}
	var (
		limited *rateLimitError
		walled  *silt.UnavailableError
		timeout *timeoutError
	)
	switch {
	case r.err == nil:
		c.Matches = len(r.matches)
		c.Status = map[detour]CheckStatus{noDetour: CheckReached, throughBrowser: CheckBrowsed, byName: CheckByName}[r.detour]
	case errors.As(r.err, &limited):
		c.Status = CheckRateLimited
		c.RateLimit = RateLimit{Source: id, Reset: limited.reset, Hint: limited.hint}
	case errors.As(r.err, &walled):
		c.Status = CheckWalled
	case errors.As(r.err, &timeout):
		c.Status = CheckTimedOut
	default:
		c.Status = CheckFailed
	}
	return c
}
