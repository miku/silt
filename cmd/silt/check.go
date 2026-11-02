package main

import (
	"context"
	"fmt"
	"io"

	"github.com/miku/silt/internal/pipeline"
	"github.com/miku/silt/sources"
)

// checkSources searches the sources cfg selects once each, without the
// caches, and prints how each answered: for --check-sources.
func checkSources(ctx context.Context, cfg pipeline.Config, w io.Writer) {
	s := sources.Searcher{Browser: cfg.Browser, PyPIIndex: cfg.PyPIIndex, Add: cfg.Add}
	// Printed first: the checks can take as long as a source may.
	fmt.Fprintf(w, "Searching each source once for %q, without the caches:\n", sources.CheckQuery.Idea)
	printChecks(w, s.Check(ctx, sources.CheckQuery, cfg.Include, cfg.Exclude))
}

// printChecks prints one line per source, and how many were reached.
func printChecks(w io.Writer, checks []sources.SourceCheck) {
	reached := 0
	for _, c := range checks {
		if c.Reached() {
			reached++
		}
		fmt.Fprintf(w, "  %-12s %5.1fs  %s\n", c.Source, c.Elapsed.Seconds(), checkNote(c))
	}
	fmt.Fprintf(w, "%d of %d sources reached.\n", reached, len(checks))
}

// checkNote says how a source answered.
func checkNote(c sources.SourceCheck) string {
	switch c.Status {
	case sources.CheckReached:
		return "reached, " + matchCount(c.Matches)
	case sources.CheckBrowsed:
		return "search walled; reached through CloakBrowser, " + matchCount(c.Matches)
	case sources.CheckByName:
		return "search walled; reached by names only, " + matchCount(c.Matches)
	case sources.CheckWalled:
		return "walled: " + c.Err.Error()
	case sources.CheckRateLimited:
		return c.RateLimit.String()
	case sources.CheckTimedOut:
		return "timed out"
	}
	return "failed: " + c.Err.Error()
}

func matchCount(n int) string {
	if n == 1 {
		return "1 match"
	}
	return fmt.Sprintf("%d matches", n)
}
