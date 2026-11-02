package rank

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/miku/silt"
	"github.com/miku/silt/internal/minilm"
	"github.com/miku/silt/model"
	"github.com/miku/silt/sources"
)

// Cosine is the cosine similarity of two equal-length vectors, in [-1, 1],
// or 0 if either is a zero vector.
func Cosine(a, b []float32) float32 {
	var dot, na, nb float32
	for i := range min(len(a), len(b)) {
		dot += a[i] * b[i]
	}
	for _, x := range a {
		na += x * x
	}
	for _, x := range b {
		nb += x * x
	}
	na, nb = float32(math.Sqrt(float64(na))), float32(math.Sqrt(float64(nb)))
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (na * nb)
}

// scoreSortLimit scores each match against the query embedding, sorts by
// similarity (stably, best first), drops negative similarities and keeps at
// most limit.
//
// Ranking is deliberately similarity-only: a match's age is reported but
// never folded into its score. An abandoned project is still prior art — the
// idea was had, and shipped — and the verdict counts close matches by this
// similarity, so deflating it for age would let a well-trodden space read as
// "Open". Recency belongs in the output, where a human can weigh it, not in
// the score.
func scoreSortLimit(queryEmb []float32, matches []model.Match, embs [][]float32, limit int) []model.Match {
	for i := range min(len(matches), len(embs)) {
		matches[i].Similarity = Cosine(queryEmb, embs[i])
	}
	slices.SortStableFunc(matches, func(a, b model.Match) int {
		return cmp.Compare(b.Similarity, a.Similarity)
	})
	// Not "< 0": like Rust's retain(>= 0.0), this also drops NaN.
	matches = slices.DeleteFunc(matches, func(m model.Match) bool { return !(m.Similarity >= 0) })
	return matches[:min(limit, len(matches))]
}

// matchText is what gets embedded for a match: "name: description", or just
// the name when there is no description.
func matchText(m model.Match) string {
	if m.Description == "" {
		return m.Name
	}
	return m.Name + ": " + m.Description
}

// Progress is told how much of a model file has been downloaded.
type Progress func(file string, done, total int64)

// Ranker is a loaded embedding model, ready to rank matches against an idea.
//
// Loading is split from ranking so that a caller can overlap the model load
// with the I/O-bound source searches. A Ranker is safe for concurrent use.
type Ranker struct {
	model *minilm.Model
}

// ModelDir is where the embedding model is cached: a stable per-user
// directory (e.g. ~/.cache/silt on Linux, ~/Library/Caches/silt on macOS), so
// that the model downloads once for the whole machine.
func ModelDir() (string, error) { return minilm.DefaultDir() }

// ModelIsCached reports whether the embedding model has been downloaded
// completely. It is used to print a one-time "downloading…" notice on the
// first run; if the cache directory can't be resolved, it assumes the model
// is there and stays quiet rather than risk a spurious notice.
func ModelIsCached() bool {
	dir, err := ModelDir()
	return err != nil || minilm.IsComplete(dir)
}

// NewRanker loads the embedding model, downloading it first (about 90 MB,
// once) if it isn't cached yet. progress, if not nil, follows the download.
// This is the expensive step: allow a second or so to load the model, and
// the download on the first run.
func NewRanker(ctx context.Context, progress Progress) (*Ranker, error) {
	dir, err := ModelDir()
	if err != nil {
		return nil, &silt.EmbeddingError{Msg: err.Error()}
	}
	if !minilm.IsComplete(dir) {
		if err := minilm.Download(ctx, dir, sources.UserAgent(), minilm.Progress(progress)); err != nil {
			return nil, &silt.EmbeddingError{Msg: err.Error()}
		}
	}
	m, err := minilm.Load(dir)
	if err != nil {
		return nil, &silt.EmbeddingError{Msg: err.Error()}
	}
	return &Ranker{model: m}, nil
}

// EmbedQuery embeds the idea. Call it while the sources are still searching.
func (r *Ranker) EmbedQuery(idea string) []float32 {
	return r.model.EmbedOne(idea)
}

// RankWith ranks matches against a precomputed query embedding, keeping at
// most limit; see scoreSortLimit. It embeds the matches concurrently.
func (r *Ranker) RankWith(queryEmb []float32, matches []model.Match, limit int) []model.Match {
	if len(matches) == 0 {
		return []model.Match{}
	}
	texts := make([]string, len(matches))
	for i, m := range matches {
		texts[i] = matchText(m)
	}
	return scoreSortLimit(queryEmb, matches, r.model.Embed(texts), limit)
}

// Rank loads the model, embeds q's idea and ranks matches, all in one call.
// With no matches it returns at once, without loading the model.
func Rank(ctx context.Context, q model.Query, matches []model.Match, limit int) ([]model.Match, error) {
	if len(matches) == 0 {
		return []model.Match{}, nil
	}
	r, err := NewRanker(ctx, nil)
	if err != nil {
		return nil, err
	}
	return r.RankWith(r.EmbedQuery(q.Idea), matches, limit), nil
}
