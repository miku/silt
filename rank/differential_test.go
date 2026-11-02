package rank

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/miku/silt/internal/minilm/minilmtest"
	"github.com/miku/silt/model"
	"github.com/miku/silt/verdict"
)

// patentRun is one line of testdata/embeddings/differential.jsonl: patent's
// --fast --json output for an idea, recorded live by
// scripts/patent-harness/record-differential.sh.
type patentRun struct {
	Query   string        `json:"query"`
	Verdict model.Verdict `json:"verdict"`
	Matches []model.Match `json:"matches"`
}

func loadPatentRuns(t *testing.T) []patentRun {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	f, err := os.Open(filepath.Join(filepath.Dir(file), "..", "testdata", "embeddings", "differential.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var runs []patentRun
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r patentRun
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return runs
}

// TestSimilaritiesMatchPatentOnRecordedSearches recomputes the similarity of
// every match patent returned for 22 ideas — real registry data — and the
// verdict level patent derived from them.
func TestSimilaritiesMatchPatentOnRecordedSearches(t *testing.T) {
	if raceEnabled {
		t.Skip("slow under the race detector")
	}
	r := &Ranker{model: minilmtest.Model(t)}
	worst, n := 0.0, 0
	for _, run := range loadPatentRuns(t) {
		matches := make([]model.Match, len(run.Matches))
		copy(matches, run.Matches)
		queryEmb := r.EmbedQuery(run.Query)
		texts := make([]string, len(matches))
		for i, m := range matches {
			texts[i] = matchText(m)
		}
		for i, emb := range r.model.Embed(texts) {
			got := Cosine(queryEmb, emb)
			d := math.Abs(float64(got - run.Matches[i].Similarity))
			worst = max(worst, d)
			n++
			if d > 1e-3 {
				t.Errorf("%q: %q has similarity %.4f, patent %.4f", run.Query, matches[i].Name, got, run.Matches[i].Similarity)
			}
			matches[i].Similarity = got
		}
		v := verdict.FromData(matches, run.Verdict.SourcesChecked, run.Verdict.SourcesFailed)
		// The headline is silt's own wording, compared with patent's in
		// verdict.TestFromDataMatchesPatent.
		if v.Level != run.Verdict.Level {
			t.Errorf("%q: level %v, patent %v", run.Query, v.Level, run.Verdict.Level)
		}
	}
	t.Logf("%d similarities, worst delta %.2e", n, worst)
}
