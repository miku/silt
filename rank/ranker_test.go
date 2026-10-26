package rank

import (
	"context"
	"math"
	"testing"

	"github.com/miku/silt/internal/minilm/minilmtest"
	"github.com/miku/silt/model"
)

func TestCosineIdenticalIsOne(t *testing.T) {
	v := []float32{1, 2, 3}
	if d := math.Abs(float64(Cosine(v, v) - 1)); d > 1e-6 {
		t.Errorf("Cosine(v, v) = %v", Cosine(v, v))
	}
}

func TestCosineOrthogonalIsZero(t *testing.T) {
	if c := Cosine([]float32{1, 0}, []float32{0, 1}); math.Abs(float64(c)) > 1e-6 {
		t.Errorf("Cosine = %v", c)
	}
}

func TestCosineZeroVectorIsZero(t *testing.T) {
	if c := Cosine([]float32{0, 0}, []float32{1, 1}); c != 0 {
		t.Errorf("Cosine = %v", c)
	}
}

func testMatch(name, desc string) model.Match {
	return model.Match{Name: name, Source: model.CratesIO, URL: "https://example.com/" + name, Description: desc}
}

// -- scoreSortLimit (pure logic, no model) --------------------------------------

func TestSSLEmptyInput(t *testing.T) {
	if got := scoreSortLimit([]float32{1, 0}, nil, nil, 10); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestSSLFillsSimilarity(t *testing.T) {
	got := scoreSortLimit([]float32{1, 0, 0}, []model.Match{testMatch("a", "something")}, [][]float32{{0.8, 0.1, 0}}, 10)
	if got[0].Similarity <= 0 {
		t.Errorf("similarity = %v", got[0].Similarity)
	}
}

func TestSSLSortsDescending(t *testing.T) {
	got := scoreSortLimit([]float32{1, 0}, []model.Match{testMatch("low", ""), testMatch("high", "")},
		[][]float32{{0.1, 0.9}, {0.9, 0.1}}, 10)
	if got[0].Name != "high" || got[1].Name != "low" || got[0].Similarity <= got[1].Similarity {
		t.Errorf("got %+v", got)
	}
}

func TestSSLTruncatesToLimit(t *testing.T) {
	got := scoreSortLimit([]float32{1, 0}, []model.Match{testMatch("a", ""), testMatch("b", ""), testMatch("c", "")},
		[][]float32{{1, 0}, {0.5, 0.5}, {0, 1}}, 2)
	if len(got) != 2 {
		t.Errorf("got %d matches, want 2", len(got))
	}
}

func TestSSLFewerThanLimitReturnsAll(t *testing.T) {
	if got := scoreSortLimit([]float32{1, 0}, []model.Match{testMatch("only", "")}, [][]float32{{1, 0}}, 10); len(got) != 1 {
		t.Errorf("got %d matches, want 1", len(got))
	}
}

func TestSSLPreservesMatchFields(t *testing.T) {
	m := testMatch("foo", "bar")
	pop := uint64(42)
	m.Popularity = &pop
	got := scoreSortLimit([]float32{1, 0}, []model.Match{m}, [][]float32{{0.9, 0.1}}, 10)
	if got[0].Name != "foo" || got[0].Description != "bar" || got[0].Popularity == nil || *got[0].Popularity != 42 {
		t.Errorf("got %+v", got[0])
	}
}

func TestSSLDropsNegativeSimilaritiesAndKeepsTiesInOrder(t *testing.T) {
	got := scoreSortLimit([]float32{1, 0},
		[]model.Match{testMatch("neg", ""), testMatch("tie1", ""), testMatch("zero", ""), testMatch("tie2", "")},
		[][]float32{{-1, 0}, {1, 1}, {0, 1}, {2, 2}}, 10)
	var names []string
	for _, m := range got {
		names = append(names, m.Name)
	}
	if len(names) != 3 || names[0] != "tie1" || names[1] != "tie2" || names[2] != "zero" {
		t.Errorf("got %v; a similarity of 0 is kept, a negative one dropped, ties keep their order", names)
	}
}

func TestMatchText(t *testing.T) {
	if got := matchText(testMatch("tokio", "An async runtime")); got != "tokio: An async runtime" {
		t.Errorf("got %q", got)
	}
	if got := matchText(testMatch("smol", "")); got != "smol" {
		t.Errorf("got %q", got)
	}
}

// -- RankWith, with the model ---------------------------------------------------

func testQuery() model.Query {
	return model.Query{Idea: "a fast async runtime for Rust", Keywords: []string{"async", "runtime"}}
}

func rankViaShared(t *testing.T, q model.Query, matches []model.Match, limit int) []model.Match {
	t.Helper()
	r := &Ranker{model: minilmtest.Model(t)}
	return r.RankWith(r.EmbedQuery(q.Idea), matches, limit)
}

func TestRankEmptyMatchesReturnsEmpty(t *testing.T) {
	r := &Ranker{} // never touches the model
	if got := r.RankWith([]float32{1}, nil, 10); got == nil || len(got) != 0 {
		t.Errorf("got %#v", got)
	}
}

func TestRankFillsPositiveSimilarityForRelatedContent(t *testing.T) {
	got := rankViaShared(t, testQuery(), []model.Match{testMatch("tokio", "An event-driven async runtime for Rust")}, 10)
	if got[0].Similarity <= 0 {
		t.Errorf("related content must have positive similarity: %v", got[0].Similarity)
	}
}

func TestRankOrdersRelevantAboveIrrelevant(t *testing.T) {
	got := rankViaShared(t, testQuery(), []model.Match{
		testMatch("recipes", "A collection of baking recipes and kitchen tips"),
		testMatch("tokio", "An event-driven non-blocking I/O platform for async Rust"),
	}, 10)
	if got[0].Name != "tokio" {
		t.Errorf("got %v first", got[0].Name)
	}
}

func TestRankRespectsLimit(t *testing.T) {
	got := rankViaShared(t, testQuery(), []model.Match{
		testMatch("a", "async runtime alpha"),
		testMatch("b", "async runtime beta"),
		testMatch("c", "async runtime gamma"),
		testMatch("d", "async runtime delta"),
	}, 2)
	if len(got) != 2 {
		t.Errorf("got %d matches, want 2", len(got))
	}
}

func TestRankReturnsSortedDescending(t *testing.T) {
	got := rankViaShared(t, testQuery(), []model.Match{
		testMatch("recipes", "Baking sourdough bread at home"),
		testMatch("smol", "A small async runtime"),
		testMatch("tokio", "An async runtime for Rust applications"),
	}, 10)
	for i := 1; i < len(got); i++ {
		if got[i-1].Similarity < got[i].Similarity {
			t.Errorf("%s (%v) should be >= %s (%v)", got[i-1].Name, got[i-1].Similarity, got[i].Name, got[i].Similarity)
		}
	}
}

// -- Rank, the one-call form; patent's rank_async ---------------------------------

func TestRankAsyncEmptyReturnsEmpty(t *testing.T) {
	// Returns before loading (or downloading) the model.
	got, err := Rank(context.Background(), testQuery(), nil, 10)
	if err != nil || len(got) != 0 {
		t.Errorf("Rank = %v, %v", got, err)
	}
}

func TestRankAsyncOrdersRelevantAboveIrrelevant(t *testing.T) {
	// Rank would download the model into silt's cache if it's missing, and
	// tests must stay off the network.
	dir, err := ModelDir()
	if err != nil || !ModelIsCached() {
		t.Skipf("the model is not in silt's cache (%s)", dir)
	}
	got, err := Rank(context.Background(), testQuery(), []model.Match{
		testMatch("recipes", "A collection of baking recipes and kitchen tips"),
		testMatch("tokio", "An event-driven non-blocking I/O platform for async Rust"),
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "tokio" {
		t.Errorf("got %v first", got[0].Name)
	}
}
