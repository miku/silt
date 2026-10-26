package pipeline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
	"github.com/miku/silt/sources"
	"github.com/miku/silt/verdict"
)

func TestValidateIdeaAcceptsANormalIdea(t *testing.T) {
	if err := ValidateIdea("a cli tool to kill a process on a given port"); err != nil {
		t.Error(err)
	}
}

func TestValidateIdeaRejectsEmptyAndVague(t *testing.T) {
	for _, idea := range []string{
		"", "   ", "ab cd",
		// Regression: short ASCII gibberish must not slip through the CJK
		// fallback (which only applies to non-ASCII, space-free scripts).
		"foobar", "123456", "hello world",
	} {
		if ValidateIdea(idea) == nil {
			t.Errorf("ValidateIdea(%q) accepted", idea)
		}
	}
}

func TestValidateIdeaRejectsFewerThanTwoNonStopwords(t *testing.T) {
	for idea, ok := range map[string]bool{
		// Zero non-stopwords — BuildQuery produces empty keywords.
		"the and for are but not":             false,
		"those would other great found first": false,
		// One non-stopword — "tool" alone generates noise results for any
		// query.
		"what does this tool have with them": false,
		"the and for tool are but not":       false,
		// Two non-stopwords — the acceptable minimum.
		"the and for tool linting what": true,
	} {
		if got := ValidateIdea(idea) == nil; got != ok {
			t.Errorf("ValidateIdea(%q) accepted = %v, want %v", idea, got, ok)
		}
	}
}

func TestValidateIdeaAcceptsNonASCIIScripts(t *testing.T) {
	// Cyrillic (space-delimited) and CJK (no spaces) must both be accepted.
	for _, idea := range []string{"инструмент для управления процессами на порту", "端口杀手命令行工具"} {
		if err := ValidateIdea(idea); err != nil {
			t.Errorf("ValidateIdea(%q) = %v", idea, err)
		}
	}
}

func TestValidateIdeaMessages(t *testing.T) {
	for idea, want := range map[string]string{
		"":               "Please provide a dev-tool idea to search for.",
		"hi":             "Too vague — describe a specific software tool or feature, e.g.\n  silt \"CLI tool that kills a process on a given port\"",
		"port port port": "Too repetitive — describe what the tool does, e.g.\n  silt \"CLI tool that kills a process on a given port\"",
		// Lowercased but not stripped: "port!" is a second distinct word.
		"Port PORT port!": "Too repetitive — describe what the tool does, e.g.\n  silt \"CLI tool that kills a process on a given port\"",
		// Distinct words, but only one that isn't a stopword.
		"the and for tool are but not": "Too vague — describe a specific software tool or feature, e.g.\n  silt \"CLI tool that kills a process on a given port\"",
	} {
		if err := ValidateIdea(idea); err == nil || err.Error() != want {
			t.Errorf("ValidateIdea(%q) = %v\nwant %q", idea, err, want)
		}
	}
}

func TestBuildQuery(t *testing.T) {
	for idea, want := range map[string][]string{
		"interactive CLI to kill whatever's on a port": {"interactive", "cli", "kill", "whatevers", "port"},
		"a cloud-native k8s operator, in Go!":          {"cloud-native", "k8s", "operator"},
		"the and for":                                  nil,
		"端口 杀手":                                        {"端口", "杀手"}, // byte length, not characters
		"C# and .NET tooling":                          {"net", "tooling"},
	} {
		q := BuildQuery(idea)
		if q.Idea != idea || !reflect.DeepEqual(q.Keywords, want) {
			t.Errorf("BuildQuery(%q) = %q, want %q", idea, q.Keywords, want)
		}
	}
}

// fakeSearch replaces the fan-out for the duration of a test.
func fakeSearch(t *testing.T, out sources.Outcome) *model.Query {
	t.Helper()
	var got model.Query
	orig := searchFiltered
	searchFiltered = func(_ context.Context, q model.Query, _, _ []model.Source) sources.Outcome {
		got = q
		return out
	}
	t.Cleanup(func() { searchFiltered = orig })
	return &got
}

func match(name, desc string) model.Match {
	return model.Match{Name: name, Source: model.Npm, URL: "https://npmjs.com/package/" + name, Description: desc}
}

func TestRunFastKeywordOnlyVerdictSeesMoreThanTheLimit(t *testing.T) {
	q := fakeSearch(t, sources.Outcome{
		Matches: []model.Match{
			match("kill-port", "kill the process on a port"),
			match("fkill", "kill processes on a port"),
			match("unrelated", "baking bread"),
		},
		Reached: []model.Source{model.Npm},
		Failed:  []model.Source{model.PyPI},
	})
	var logs []string
	res, err := Run(context.Background(), "kill process port", Config{Fast: true, KeywordOnly: true, Limit: 1},
		func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q.Keywords, []string{"kill", "process", "port"}) {
		t.Errorf("searched for %q", q.Keywords)
	}
	// The verdict is computed before truncating: two strong matches make
	// the space Crowded even though only one is returned.
	if res.Verdict.Level != model.Crowded {
		t.Errorf("level = %v, want Crowded", res.Verdict.Level)
	}
	if len(res.Matches) != 1 || res.Matches[0].Name != "kill-port" {
		t.Errorf("matches = %+v", res.Matches)
	}
	if !reflect.DeepEqual(res.Verdict.SourcesFailed, []model.Source{model.PyPI}) {
		t.Errorf("failed sources must reach the verdict: %v", res.Verdict.SourcesFailed)
	}
	want := []string{"   3 matches from 1 sources in", "Keyword-ranked to top 2 in", "--fast: skipping the LLM (verdict from similarity data only)"}
	if len(logs) != len(want) {
		t.Fatalf("logs = %q", logs)
	}
	for i := range want {
		if !strings.HasPrefix(logs[i], want[i]) {
			t.Errorf("log %d = %q, want prefix %q", i, logs[i], want[i])
		}
	}
	if !strings.HasSuffix(logs[0], "s: npm") {
		t.Errorf("log = %q", logs[0])
	}
}

func TestRunSkipsTheVerdictBelowMinRelevance(t *testing.T) {
	fakeSearch(t, sources.Outcome{Matches: []model.Match{match("bread", "baking bread")}, Reached: []model.Source{model.Npm}})
	var logs []string
	res, err := Run(context.Background(), "kill process port", Config{KeywordOnly: true, Limit: 50},
		func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Level != model.Open || !strings.HasPrefix(res.Verdict.Headline, "Nothing relevant turned up") ||
		res.Verdict.Caveat != verdict.Caveat || res.Verdict.Gaps == nil {
		t.Errorf("verdict = %+v", res.Verdict)
	}
	if got := logs[len(logs)-1]; got != "warning: best similarity 0.00 < 0.35 — skipping verdict" {
		t.Errorf("log = %q", got)
	}
}

func TestRunFallsBackToSearchOnlyVerdictWhenTheLLMFails(t *testing.T) {
	fakeSearch(t, sources.Outcome{Matches: []model.Match{match("kill-port", "kill a port")}, Reached: []model.Source{model.Npm}})
	var logs []string
	res, err := Run(context.Background(), "kill process port", Config{KeywordOnly: true, Limit: 50},
		func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Headline != "Verdict unavailable — results are ranked by semantic similarity only." {
		t.Errorf("headline = %q", res.Verdict.Headline)
	}
	if got := logs[len(logs)-1]; got != "   showing results without an AI verdict." {
		t.Errorf("log = %q", got)
	}
}

func TestRunWithoutKeywordOnlyNeedsTheEmbedder(t *testing.T) {
	fakeSearch(t, sources.Outcome{})
	_, err := Run(context.Background(), "kill process port", Config{Fast: true, Limit: 50}, nil)
	var embedding *silt.EmbeddingError
	if !errors.As(err, &embedding) {
		t.Errorf("err = %v, want an EmbeddingError until M5", err)
	}
}
