package verdict

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miku/silt/model"
)

func checked() []model.Source { return []model.Source{model.Npm, model.CratesIO} }

func ptr[T any](v T) *T { return &v }

func sampleMatches() []model.Match {
	return []model.Match{
		{
			Name: "kill-port", Source: model.Npm, URL: "https://npmjs.com/package/kill-port",
			Description: "Kill process on a port", Popularity: ptr(uint64(50_000)), Similarity: 0.85,
		},
		{
			Name: "fkill-cli", Source: model.Npm, URL: "https://npmjs.com/package/fkill-cli",
			Description: "Fabulously kill processes", Popularity: ptr(uint64(10_000)), Similarity: 0.60,
		},
	}
}

func withSimilarities(sims ...float32) []model.Match {
	var ms []model.Match
	for _, s := range sims {
		ms = append(ms, model.Match{Name: "m", Source: model.CratesIO, Similarity: s})
	}
	return ms
}

func TestFromDataFloorsLevelAgainstSimilarity(t *testing.T) {
	// Two matches at 0.85 and 0.60 => at least two >= 0.55 => Crowded.
	v := FromData(sampleMatches(), checked(), nil)
	if v.Level != model.Crowded {
		t.Errorf("level = %v, want Crowded", v.Level)
	}
	if v.Caveat != Caveat || !reflect.DeepEqual(v.SourcesChecked, checked()) {
		t.Errorf("verdict = %+v", v)
	}
}

func TestFromDataOpenWhenNothingClose(t *testing.T) {
	v := FromData(nil, []model.Source{model.GitHub}, nil)
	if v.Level != model.Open || v.Caveat != Caveat {
		t.Errorf("verdict = %+v", v)
	}
}

func TestFromDataFloorsSingleVeryStrongMatchToCrowded(t *testing.T) {
	// One >= 0.70 match => Crowded.
	v := FromData([]model.Match{{
		Name: "twin", Source: model.CratesIO, URL: "https://crates.io/crates/twin",
		Description: "Nearly the same idea", Similarity: 0.72,
	}}, checked(), nil)
	if v.Level != model.Crowded || v.Caveat != Caveat {
		t.Errorf("verdict = %+v", v)
	}
}

func TestFromDataNeverAssertsAbsence(t *testing.T) {
	// Even with zero matches the headline must never claim the idea doesn't
	// exist anywhere.
	lower := strings.ToLower(FromData(nil, []model.Source{model.GitHub}, nil).Headline)
	for _, phrase := range []string{"does not exist", "no prior art", "never been", "unprecedented"} {
		if strings.Contains(lower, phrase) {
			t.Errorf("absence claim in the headline: %q", lower)
		}
	}
}

func TestFromDataThreadsFailedSources(t *testing.T) {
	v := FromData(sampleMatches(), checked(), []model.Source{model.PyPI})
	if !reflect.DeepEqual(v.SourcesFailed, []model.Source{model.PyPI}) {
		t.Errorf("sources_failed = %v", v.SourcesFailed)
	}
}

func TestLevelThresholds(t *testing.T) {
	for _, tc := range []struct {
		sims []float32
		want model.Saturation
	}{
		{nil, model.Open},
		{[]float32{0.69}, model.Open},
		{[]float32{0.70}, model.Crowded},
		{[]float32{0.55, 0.549}, model.Open},
		{[]float32{0.55, 0.55}, model.Crowded},
		{[]float32{0.6, 0.6, 0.6, 0.6}, model.Crowded},
		{[]float32{0.6, 0.6, 0.6, 0.6, 0.6}, model.Saturated},
		{[]float32{0.99, 0.99, 0.99, 0.99, 0.59}, model.Crowded},
	} {
		if got := levelOf(withSimilarities(tc.sims...)); got != tc.want {
			t.Errorf("levelOf(%v) = %v, want %v", tc.sims, got, tc.want)
		}
	}
}

func TestHeadlines(t *testing.T) {
	for _, tc := range []struct {
		sims []float32
		want string
	}{
		{[]float32{0.9, 0.8, 0.7, 0.6, 0.6, 0.6}, "We believe this space is saturated: 6 closely-related tools turned up in the sources checked."},
		{[]float32{0.72}, "We believe this space is crowded: 1 closely-related tool turned up in the sources checked."},
		{[]float32{0.6, 0.6}, "We believe this space is crowded: 2 closely-related tools turned up in the sources checked."},
		{nil, "We believe this space is open: nothing close turned up in the sources checked. Keep looking before committing."},
		{[]float32{0.54}, "We believe this space is open: nothing close turned up in the sources checked. Keep looking before committing."},
		{[]float32{0.56}, "We believe this space is open: 1 closely-related tool turned up in the sources checked. Worth a look before committing."},
	} {
		if got := FromData(withSimilarities(tc.sims...), nil, nil).Headline; got != tc.want {
			t.Errorf("headline for %v = %q\nwant %q", tc.sims, got, tc.want)
		}
	}
}
