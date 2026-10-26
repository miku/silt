// Package pipeline is the search → rank → verdict pipeline shared by the CLI
// and the TUI.
package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miku/silt/llm"
	"github.com/miku/silt/llm/ollama"
	"github.com/miku/silt/llm/openai"
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
	// Progress, if not nil, follows the download of the embedding model on
	// the first run.
	Progress rank.Progress
}

// Result is a run's verdict and ranked matches.
type Result struct {
	Verdict model.Verdict
	Matches []model.Match
}

// ranker is what Run needs of a *rank.Ranker.
type ranker interface {
	EmbedQuery(idea string) []float32
	RankWith(queryEmb []float32, matches []model.Match, limit int) []model.Match
}

// searchFiltered runs the fan-out, newRanker loads the embedding model, and
// ollamaEndpoint is where the default LLM backend listens; tests replace them
// to stay off the network and away from a local Ollama.
var (
	searchFiltered = sources.SearchFiltered
	newRanker      = func(ctx context.Context, progress rank.Progress) (ranker, error) {
		r, err := rank.NewRanker(ctx, progress)
		if err != nil {
			return nil, err
		}
		return r, nil
	}
	ollamaEndpoint = ollama.DefaultEndpoint
)

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

	// ── Phase 1: search sources (+ load the embedding model if needed) ──
	// Rank to max(limit, DefaultLimit) so that the floor and the verdict
	// prompt see enough data to make an accurate call even when the limit is
	// small. The result is truncated to the limit after the verdict.
	start := time.Now()
	evalLimit := max(cfg.Limit, rank.DefaultLimit)
	logSearch := func(out sources.Outcome) {
		log(fmt.Sprintf("   %d matches from %d sources in %.1fs: %s",
			len(out.Matches), len(out.Reached), time.Since(start).Seconds(), sourceNames(out.Reached)))
	}

	var (
		out    sources.Outcome
		ranked []model.Match
	)
	if cfg.KeywordOnly {
		out = searchFiltered(ctx, q, cfg.Include, cfg.Exclude)
		logSearch(out)
		rankStart := time.Now()
		ranked = rank.ByKeywords(q, out.Matches, evalLimit)
		log(fmt.Sprintf("Keyword-ranked to top %d in %.1fs", len(ranked), time.Since(rankStart).Seconds()))
	} else {
		// The model load (and, on the first run, its download) overlaps the
		// I/O-bound fan-out.
		type loaded struct {
			r        ranker
			queryEmb []float32
			err      error
		}
		ch := make(chan loaded, 1)
		go func() {
			r, err := newRanker(ctx, cfg.Progress)
			if err != nil {
				ch <- loaded{err: err}
				return
			}
			ch <- loaded{r: r, queryEmb: r.EmbedQuery(q.Idea)}
		}()
		out = searchFiltered(ctx, q, cfg.Include, cfg.Exclude)
		l := <-ch
		if l.err != nil {
			return Result{}, l.err
		}
		logSearch(out)

		// ── Phase 2: rank (embed the matches + cosine sort) ─────────────
		rankStart := time.Now()
		ranked = l.r.RankWith(l.queryEmb, out.Matches, evalLimit)
		log(fmt.Sprintf("Ranked to top %d in %.1fs", len(ranked), time.Since(rankStart).Seconds()))
	}

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
		modelName := ollama.DefaultModel
		if cfg.Model != nil {
			modelName = *cfg.Model
		}
		var backend llm.LLM = ollama.New(ollamaEndpoint, modelName)
		if cfg.APIBase != nil {
			backend = openai.New(*cfg.APIBase, modelName, cfg.APIKey)
		}
		verdictStart := time.Now()
		log(fmt.Sprintf("Generating verdict via %s (%s)...", backend.Label(), modelName))
		var err error
		if v, err = verdict.Assess(ctx, backend, q, ranked, out.Reached, out.Failed); err != nil {
			// A failing LLM degrades the run to the search-only verdict
			// rather than aborting it.
			log("warning: " + err.Error())
			log("   showing results without an AI verdict.")
			v = fallback("Verdict unavailable — results are ranked by semantic similarity only.")
		} else {
			log(fmt.Sprintf("   verdict in %.1fs", time.Since(verdictStart).Seconds()))
		}
	}

	return Result{Verdict: v, Matches: ranked[:min(cfg.Limit, len(ranked))]}, nil
}
