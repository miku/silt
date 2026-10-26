package minilm

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
)

// golden is testdata/embeddings/golden.json, recorded from patent's Ranker
// by scripts/patent-harness.
type golden struct {
	Texts      []string    `json:"texts"`
	TokenIDs   [][]int32   `json:"token_ids"`
	Embeddings [][]float32 `json:"embeddings"`
	Cosines    [][]float32 `json:"cosines"`
	Rankings   []struct {
		Idea   string `json:"idea"`
		Ranked []struct {
			Name       string  `json:"name"`
			Similarity float32 `json:"similarity"`
		} `json:"ranked"`
	} `json:"rankings"`
}

func testdata(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "embeddings", name)
}

func loadGolden(t testing.TB) golden {
	t.Helper()
	b, err := os.ReadFile(testdata("golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// modelDir finds the model files: $SILT_MINILM_DIR, silt's cache, or patent's.
// Tests that need the model are skipped without one; go run ./scripts/fetchmodel
// downloads it.
func modelDir(t testing.TB) string {
	t.Helper()
	if dir := os.Getenv("SILT_MINILM_DIR"); dir != "" {
		return dir
	}
	if dir, err := DefaultDir(); err == nil && IsComplete(dir) {
		return dir
	}
	if dir := patentModelDir(); dir != "" {
		return dir
	}
	t.Skip("no all-MiniLM-L6-v2 model; run go run ./scripts/fetchmodel or set SILT_MINILM_DIR")
	return ""
}

// patentModelDir is where fastembed cached the model for patent, or "".
func patentModelDir() string {
	home, _ := os.UserHomeDir()
	dirs, _ := filepath.Glob(filepath.Join(home, ".cache/patent/fastembed/models--Qdrant--all-MiniLM-L6-v2-onnx/snapshots/*"))
	for _, dir := range dirs {
		if IsComplete(dir) {
			return dir
		}
	}
	return ""
}

func loadTokenizer(t testing.TB) *Tokenizer {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(modelDir(t), "tokenizer.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := NewTokenizer(b)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestTokenizerMatchesHuggingFace(t *testing.T) {
	g := loadGolden(t)
	tok := loadTokenizer(t)
	for i, text := range g.Texts {
		if got := tok.Encode(text); !slices.Equal(got, g.TokenIDs[i]) {
			t.Errorf("text %d %.60q:\n got %v\nwant %v", i, text, got, g.TokenIDs[i])
		}
	}
}

func TestTokenizerMatchesAddedTokensInTheRawText(t *testing.T) {
	tok := loadTokenizer(t)
	// [MASK] is 103 wherever it appears, even glued to a word; the words
	// around it are tokenized as usual.
	got := tok.Encode("fill[MASK] in")
	if len(got) < 4 || got[0] != 101 || got[len(got)-1] != 102 || !slices.Contains(got, 103) {
		t.Errorf("Encode = %v", got)
	}
}

// sharedModel loads the model once for all tests and benchmarks.
var sharedModel = sync.OnceValues(func() (*Model, error) {
	dir := os.Getenv("SILT_MINILM_DIR")
	if dir == "" {
		return nil, nil
	}
	return Load(dir)
})

func loadModel(t testing.TB) *Model {
	t.Helper()
	if os.Getenv("SILT_MINILM_DIR") == "" {
		t.Setenv("SILT_MINILM_DIR", modelDir(t))
	}
	m, err := sharedModel()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// TestEmbeddingsMatchPatent is the D2 acceptance test: the thresholds
// silt inherits (0.35, 0.55, 0.60, 0.70) are calibrated on patent's
// similarities, so every pairwise cosine must land within 1e-3 of them.
func TestEmbeddingsMatchPatent(t *testing.T) { checkGoldenEmbeddings(t) }

func checkGoldenEmbeddings(t *testing.T) {
	if raceEnabled {
		t.Skip("slow under the race detector; TestEmbedIsSafeForConcurrentUse covers concurrency")
	}
	g := loadGolden(t)
	m := loadModel(t)
	embs := m.Embed(g.Texts)
	worst, worstSelf := 0.0, 0.0
	for i := range embs {
		d := 1 - cosine(embs[i], g.Embeddings[i])
		worstSelf = max(worstSelf, d)
		if d > 1e-5 {
			t.Errorf("text %d %.50q: 1 - cos(silt, patent) = %.2e", i, g.Texts[i], d)
		}
		for j := range embs {
			d := math.Abs(cosine(embs[i], embs[j]) - float64(g.Cosines[i][j]))
			worst = max(worst, d)
			if d > 1e-3 {
				t.Errorf("cosine(%d, %d) is off by %.2e", i, j, d)
			}
		}
	}
	t.Logf("worst 1 - cos(silt, patent): %.2e; worst pairwise cosine delta: %.2e", worstSelf, worst)
}

// inputs is testdata/embeddings/inputs.json.
type inputs struct {
	Rankings []struct {
		Idea    string `json:"idea"`
		Matches []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"matches"`
	} `json:"rankings"`
}

func loadInputs(t testing.TB) inputs {
	t.Helper()
	b, err := os.ReadFile(testdata("inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var in inputs
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

// matchText is the text rank_with embeds for a match.
func matchText(name, desc string) string {
	if desc == "" {
		return name
	}
	return name + ": " + desc
}

// TestRankWithSimilaritiesMatchPatent checks the similarities patent's
// rank_with computes, which embeds the matches in one batch padded to the
// longest text.
func TestRankWithSimilaritiesMatchPatent(t *testing.T) {
	if raceEnabled {
		t.Skip("slow under the race detector")
	}
	g := loadGolden(t)
	in := loadInputs(t)
	m := loadModel(t)
	worst := 0.0
	defer func() { t.Logf("worst similarity delta: %.2e", worst) }()
	for r, ranking := range in.Rankings {
		want := make(map[string]float32)
		for _, s := range g.Rankings[r].Ranked {
			want[s.Name] = s.Similarity
		}
		q := m.EmbedOne(ranking.Idea)
		for _, match := range ranking.Matches {
			got := cosine(q, m.EmbedOne(matchText(match.Name, match.Description)))
			w, ok := want[match.Name]
			if !ok {
				// rank_with drops negative similarities.
				if got >= 0 {
					t.Errorf("%q: %q has similarity %.4f, but patent dropped it as negative", ranking.Idea, match.Name, got)
				}
				continue
			}
			d := math.Abs(got - float64(w))
			worst = max(worst, d)
			if d > 1e-3 {
				t.Errorf("%q: %q has similarity %.4f, patent %.4f", ranking.Idea, match.Name, got, w)
			}
		}
	}
}

// benchTexts are n match texts cycled from the inputs, as the harness's
// bench command builds them: without the one over-long description, which
// would make patent pad every text in its batch to 512 tokens.
func benchTexts(t testing.TB, n int) []string {
	in := loadInputs(t)
	var pool []string
	for _, r := range in.Rankings {
		for _, m := range r.Matches {
			if len(m.Description) < 1000 {
				pool = append(pool, matchText(m.Name+" %d", m.Description))
			}
		}
	}
	texts := make([]string, n)
	for i := range texts {
		texts[i] = fmt.Sprintf(pool[i%len(pool)], i)
	}
	return texts
}

// BenchmarkEmbed300 embeds 300 realistic match texts, which patent ranks in
// about 1.2 s on an 8-core laptop.
func BenchmarkEmbed300(b *testing.B) {
	m := loadModel(b)
	texts := benchTexts(b, 300)
	for b.Loop() {
		m.Embed(texts)
	}
}

func BenchmarkLoad(b *testing.B) {
	dir := modelDir(b)
	for b.Loop() {
		if _, err := Load(dir); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEmbedIdea is the query side: one short idea.
func BenchmarkEmbedIdea(b *testing.B) {
	m := loadModel(b)
	for b.Loop() {
		m.EmbedOne("interactive cli to kill whatever's on a port")
	}
}

func TestEmbedIsSafeForConcurrentUse(t *testing.T) {
	m := loadModel(t)
	texts := []string{"kill-port", "fkill-cli: Fabulously kill processes", "tokio", "smol: A small and fast async runtime", "", "端口"}
	texts = append(texts, texts...)
	got := m.Embed(texts)
	for i, text := range texts {
		if !slices.Equal(got[i], m.EmbedOne(text)) {
			t.Errorf("Embed and EmbedOne differ for %q", text)
		}
	}
}
