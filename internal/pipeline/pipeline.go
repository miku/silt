// Package pipeline is the search → rank → verdict pipeline shared by the CLI
// and the TUI.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
	"github.com/miku/silt/rank"
	"github.com/miku/silt/sources"
	"github.com/miku/silt/verdict"
)

// MinRelevance is the similarity below which matches are noise, not signal:
// the LLM verdict is skipped entirely, to avoid hallucinating a judgement
// from irrelevant results.
const MinRelevance float32 = 0.35

// Config configures a run. Nil pointers are settings left unset.
type Config struct {
	APIBase     *string
	APIKey      *string
	Model       *string
	Fast        bool
	KeywordOnly bool
	// Limit is how many matches to keep after ranking.
	Limit int
	// Include, if not nil, replaces the auto-detected sources.
	Include []model.Source
	Exclude []model.Source
}

// Result is a run's verdict and ranked matches.
type Result struct {
	Verdict model.Verdict
	Matches []model.Match
}

// errEmbeddingNotPorted and errLLMNotPorted stand in for the parts of patent
// that silt has not ported yet (M5 and M6 in notes/todo.md).
var (
	errEmbeddingNotPorted = &silt.EmbeddingError{Msg: "semantic ranking is not ported to silt yet; pass --keyword-only"}
	errLLMNotPorted       = errors.New("LLM verdicts are not ported to silt yet; pass --fast for a verdict from the similarity data")
)

// searchFiltered runs the fan-out; tests replace it to stay off the network.
var searchFiltered = sources.SearchFiltered

func sourceNames(ss []model.Source) string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.String()
	}
	return strings.Join(names, ", ")
}

// Run searches the sources for idea, ranks the matches and derives a
// verdict, reporting progress through log. Individual sources failing is not
// an error; they are listed in the verdict's SourcesFailed.
func Run(ctx context.Context, idea string, cfg Config, log func(string)) (Result, error) {
	if log == nil {
		log = func(string) {}
	}
	q := BuildQuery(idea)

	// ── Phase 1: search sources ─────────────────────────────────────────
	if !cfg.KeywordOnly {
		return Result{}, errEmbeddingNotPorted
	}
	start := time.Now()
	out := searchFiltered(ctx, q, cfg.Include, cfg.Exclude)
	log(fmt.Sprintf("   %d matches from %d sources in %.1fs: %s",
		len(out.Matches), len(out.Reached), time.Since(start).Seconds(), sourceNames(out.Reached)))

	// ── Phase 2: rank ───────────────────────────────────────────────────
	// Rank to max(limit, DefaultLimit) so that the floor and the verdict
	// prompt see enough data to make an accurate call even when the limit is
	// small. The result is truncated to the limit after the verdict.
	rankStart := time.Now()
	ranked := rank.ByKeywords(q, out.Matches, max(cfg.Limit, rank.DefaultLimit))
	log(fmt.Sprintf("Keyword-ranked to top %d in %.1fs", len(ranked), time.Since(rankStart).Seconds()))

	// ── Phase 3: relevance gate + verdict ───────────────────────────────
	fallback := func(headline string) model.Verdict {
		return model.Verdict{
			Level:          model.Open,
			Headline:       headline,
			Gaps:           []string{},
			SourcesChecked: out.Reached,
			SourcesFailed:  out.Failed,
			Caveat:         verdict.Caveat,
		}
	}
	var bestSim float32
	if len(ranked) > 0 {
		bestSim = ranked[0].Similarity
	}
	var v model.Verdict
	switch {
	case cfg.Fast:
		log("--fast: skipping the LLM (verdict from similarity data only)")
		v = verdict.FromData(ranked, out.Reached, out.Failed)
	case bestSim < MinRelevance:
		log(fmt.Sprintf("warning: best similarity %.2f < %.2f — skipping verdict", bestSim, MinRelevance))
		v = fallback("Nothing relevant turned up in the sources checked. " +
			"The query may not describe a recognized software tool — " +
			"try rephrasing with specific technical terms.")
	default:
		// As for any LLM failure, fall back to the search-only verdict
		// rather than aborting.
		log("warning: " + errLLMNotPorted.Error())
		log("   showing results without an AI verdict.")
		v = fallback("Verdict unavailable — results are ranked by semantic similarity only.")
	}

	return Result{Verdict: v, Matches: ranked[:min(cfg.Limit, len(ranked))]}, nil
}
