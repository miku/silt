package verdict

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miku/silt/model"
)

func query() model.Query {
	return model.Query{Idea: "a cli to kill processes on a port", Keywords: []string{"kill", "port"}}
}

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

// -- BuildPrompt ---------------------------------------------------------------

func TestPromptContainsTheIdea(t *testing.T) {
	if p := BuildPrompt(query(), sampleMatches(), checked()); !strings.Contains(p, "a cli to kill processes on a port") {
		t.Error("prompt must include the user's idea")
	}
}

func TestPromptContainsMatchNames(t *testing.T) {
	p := BuildPrompt(query(), sampleMatches(), checked())
	if !strings.Contains(p, "kill-port") || !strings.Contains(p, "fkill-cli") {
		t.Error("prompt must name the matches")
	}
}

func TestPromptIncludesPopularityAndURLs(t *testing.T) {
	// #60: the model should see how popular/real each match is and where it
	// lives, so it can weight firmly-established prior art.
	p := BuildPrompt(query(), sampleMatches(), checked())
	if !strings.Contains(p, "popularity 50000") {
		t.Error("prompt must include the match popularity figure")
	}
	if !strings.Contains(p, "https://npmjs.com/package/kill-port") {
		t.Error("prompt must include the match URL")
	}
}

func line(t *testing.T, prompt, substr string) string {
	t.Helper()
	for _, l := range strings.Split(prompt, "\n") {
		if strings.Contains(l, substr) {
			return l
		}
	}
	t.Fatalf("no line with %q in the prompt", substr)
	return ""
}

func TestPromptDatesTheMatchesWhoseSourcePublishesOne(t *testing.T) {
	// #29: the model weighs prior art better when it can see how live each
	// match is — and an undated match must read as "no date published", never
	// as a match that happens to be old.
	matches := sampleMatches()
	matches[0].LastUpdated = ptr("2015-03-01T00:00:00Z")
	p := BuildPrompt(query(), matches, checked())
	if dated := line(t, p, "kill-port"); !strings.Contains(dated, "updated") || !strings.Contains(dated, "years ago") {
		t.Errorf("a dated match must carry its age: %s", dated)
	}
	if undated := line(t, p, "fkill-cli"); strings.Contains(undated, "updated") {
		t.Errorf("an undated match must claim nothing about its age: %s", undated)
	}
}

func TestPromptWarnsThatAgeIsNotAReasonToDiscountPriorArt(t *testing.T) {
	// Integrity: staleness is colour on the prior art, not a licence to call a
	// crowded space open. If the guidance ever drops out of the prompt, a
	// model is free to reason "it's old, therefore it doesn't count".
	lower := strings.ToLower(BuildPrompt(query(), sampleMatches(), checked()))
	if !strings.Contains(lower, "never as a reason to discount") {
		t.Error("prompt must tell the model not to discount prior art for being stale")
	}
	if !strings.Contains(lower, "not evidence of staleness") {
		t.Error("prompt must explain that an absent date is not a staleness signal")
	}
}

func TestPromptForbidsAssertingAbsence(t *testing.T) {
	lower := strings.ToLower(BuildPrompt(query(), sampleMatches(), checked()))
	if !strings.Contains(lower, "never") && !strings.Contains(lower, "do not") && !strings.Contains(lower, "must not") {
		t.Error("prompt must forbid claiming non-existence")
	}
}

func TestPromptRequiresJSONOutput(t *testing.T) {
	if !strings.Contains(strings.ToLower(BuildPrompt(query(), sampleMatches(), checked())), "json") {
		t.Error("prompt must ask for JSON output")
	}
}

func TestPromptWithNoMatchesStillValid(t *testing.T) {
	p := BuildPrompt(query(), nil, checked())
	if !strings.Contains(p, "a cli to kill processes on a port") || !strings.Contains(p, "No matches were found in the sources checked.") {
		t.Errorf("prompt = %q", p)
	}
}

func TestPromptNamesOnlyTheSourcesActuallyChecked(t *testing.T) {
	// Integrity: the model must only be told about coverage that really
	// happened. With HN not in the reached set, it must not be named.
	p := BuildPrompt(query(), sampleMatches(), checked())
	if !strings.Contains(p, "npm") || !strings.Contains(p, "crates.io") {
		t.Error("must name the reached sources")
	}
	if strings.Contains(p, "Hacker News") {
		t.Error("must not name a source that wasn't reached")
	}
	if p := BuildPrompt(query(), nil, nil); !strings.Contains(p, "implementations: the selected open-source registries.") {
		t.Error("with no reached sources the prompt must not name any")
	}
}

func TestPromptUsesSkepticalReviewerPersona(t *testing.T) {
	lower := strings.ToLower(BuildPrompt(query(), sampleMatches(), checked()))
	if !strings.Contains(lower, "skeptic") && !strings.Contains(lower, "default assumption") {
		t.Error("prompt should frame the reviewer as skeptical")
	}
}

func TestPromptListsTheTop15AndCountsTheRest(t *testing.T) {
	var matches []model.Match
	for range 18 {
		matches = append(matches, model.Match{Name: "x", Source: model.Npm, Similarity: 0.5})
	}
	p := BuildPrompt(query(), matches, checked())
	if n := strings.Count(p, "- **x**"); n != 15 {
		t.Errorf("listed %d matches, want 15", n)
	}
	if !strings.Contains(p, "- … and 3 more with lower similarity\n") {
		t.Error("missing the count of unlisted matches")
	}
	if !strings.Contains(p, "Top-10 average similarity: 0.50 ") {
		t.Error("missing the top-10 average")
	}
	// A blank URL is left out rather than rendered as a dangling dash.
	if !strings.Contains(p, "- **x** (npm, sim 0.50): \n") {
		t.Errorf("match line = %q", line(t, p, "- **x**"))
	}
}

// -- FromData: the default, no-LLM path ------------------------------------------

func TestFromDataFloorsLevelAgainstSimilarity(t *testing.T) {
	// Two matches at 0.85 and 0.60 => at least two >= 0.55 => Crowded,
	// derived from the similarity data alone with no model in the loop. The
	// no-LLM path must never under-rate a populated space into a green Open.
	v := FromData(sampleMatches(), checked(), nil)
	if v.Level != model.Crowded {
		t.Errorf("level = %v, want Crowded", v.Level)
	}
	if len(v.Gaps) != 0 {
		t.Error("no model => no gaps")
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
	// The no-LLM path routes through the same floor: one >= 0.70 match =>
	// Crowded.
	v := FromData([]model.Match{{
		Name: "twin", Source: model.CratesIO, URL: "https://crates.io/crates/twin",
		Description: "Nearly the same idea", Similarity: 0.72,
	}}, checked(), nil)
	if v.Level != model.Crowded || len(v.Gaps) != 0 || v.Caveat != Caveat {
		t.Errorf("verdict = %+v", v)
	}
}

func TestFromDataNeverAssertsAbsence(t *testing.T) {
	// The integrity rule holds on the no-LLM path too: even with zero matches
	// the headline must never claim the idea doesn't exist anywhere.
	lower := strings.ToLower(FromData(nil, []model.Source{model.GitHub}, nil).Headline)
	for _, phrase := range []string{"does not exist", "no prior art", "never been", "unprecedented"} {
		if strings.Contains(lower, phrase) {
			t.Errorf("absence claim in fast headline: %q", lower)
		}
	}
}

func TestFromDataThreadsFailedSources(t *testing.T) {
	v := FromData(sampleMatches(), checked(), []model.Source{model.PyPI})
	if !reflect.DeepEqual(v.SourcesFailed, []model.Source{model.PyPI}) {
		t.Errorf("sources_failed = %v", v.SourcesFailed)
	}
}

func TestFloorLevelThresholds(t *testing.T) {
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
		if got := floorLevel(model.Open, withSimilarities(tc.sims...)); got != tc.want {
			t.Errorf("floorLevel(%v) = %v, want %v", tc.sims, got, tc.want)
		}
	}
	// The floor only ever raises a level.
	if got := floorLevel(model.Saturated, nil); got != model.Saturated {
		t.Errorf("floorLevel lowered Saturated to %v", got)
	}
}

func TestDataHeadlines(t *testing.T) {
	for _, tc := range []struct {
		level model.Saturation
		sims  []float32
		want  string
	}{
		{model.Saturated, []float32{0.9, 0.8, 0.7, 0.6, 0.6, 0.6}, "Saturated — 6 closely-related tools turned up in the sources checked."},
		{model.Crowded, []float32{0.72}, "Crowded — 1 closely-related tool turned up in the sources checked."},
		{model.Crowded, []float32{0.6, 0.6}, "Crowded — 2 closely-related tools turned up in the sources checked."},
		{model.Open, nil, "Nothing close turned up in the sources checked — keep looking before committing."},
		{model.Open, []float32{0.56}, "1 closely-related tool turned up, but the space still looks open in the sources checked. Worth a look before committing."},
	} {
		if got := dataHeadline(tc.level, withSimilarities(tc.sims...)); got != tc.want {
			t.Errorf("dataHeadline(%v, %v) = %q\nwant %q", tc.level, tc.sims, got, tc.want)
		}
	}
}

func TestBeliefHeadlines(t *testing.T) {
	for _, tc := range []struct {
		level model.Saturation
		sims  []float32
		want  string
	}{
		{model.Saturated, []float32{0.9, 0.8, 0.7, 0.6, 0.6, 0.6}, "We believe this space is saturated: 6 closely-related tools turned up in the sources checked."},
		{model.Crowded, []float32{0.72}, "We believe this space is crowded: 1 closely-related tool turned up in the sources checked."},
		{model.Crowded, []float32{0.6, 0.6}, "We believe this space is crowded: 2 closely-related tools turned up in the sources checked."},
		{model.Open, nil, "We believe this space is open: nothing close turned up in the sources checked. Keep looking before committing."},
		{model.Open, []float32{0.56}, "We believe this space is open: 1 closely-related tool turned up in the sources checked. Worth a look before committing."},
	} {
		got := beliefHeadline(tc.level, withSimilarities(tc.sims...))
		if got != tc.want {
			t.Errorf("beliefHeadline(%v, %v) = %q\nwant %q", tc.level, tc.sims, got, tc.want)
		}
		// The guard the LLM's headlines go through would let it stand.
		if containsAbsencePhrase(got) || (len(tc.sims) > 0 && claimsNoMatch(got)) {
			t.Errorf("beliefHeadline(%v, %v) = %q, which the guards would replace", tc.level, tc.sims, got)
		}
	}
}

func TestFromDataWordsTheLevelAsABelief(t *testing.T) {
	v := FromData(sampleMatches(), checked(), nil)
	if want := "We believe this space is crowded: 2 closely-related tools turned up in the sources checked."; v.Headline != want {
		t.Errorf("headline = %q, want %q", v.Headline, want)
	}
}
