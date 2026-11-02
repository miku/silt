package pipeline

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miku/silt"
	"github.com/miku/silt/model"
	"github.com/miku/silt/rank"
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
	searchFiltered = func(_ context.Context, _ sources.Searcher, q model.Query, _, _ []model.Source) sources.Outcome {
		got = q
		return out
	}
	t.Cleanup(func() { searchFiltered = orig })
	return &got
}

func match(name, desc string) model.Match {
	return model.Match{Name: name, Source: model.Npm, URL: "https://npmjs.com/package/" + name, Description: desc}
}

func TestRunKeywordOnlyVerdictSeesMoreThanTheLimit(t *testing.T) {
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
	res, err := Run(context.Background(), "kill process port", Config{KeywordOnly: true, Limit: 1},
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
	want := []string{"   3 matches from 1 sources in", "Keyword-ranked to top 2 in", "Verdict from the similarity data (--llm-verdict asks an LLM instead)"}
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
	res, err := Run(context.Background(), "kill process port", Config{LLMVerdict: true, KeywordOnly: true, Limit: 50},
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

// nopBrowser is a sources.Browser that is never asked for a page.
type nopBrowser struct{}

func (nopBrowser) DumpDOM(context.Context, string) (string, error) { return "", nil }

func TestRunPassesTheCachesAndBrowserAndSaysHowSourcesWereReached(t *testing.T) {
	cache := &sources.Cache{Dir: t.TempDir(), TTL: 90 * time.Minute}
	catalogs := &sources.CatalogCache{Dir: t.TempDir(), TTL: 24 * time.Hour}
	var got sources.Searcher
	orig := searchFiltered
	searchFiltered = func(_ context.Context, s sources.Searcher, _ model.Query, _, _ []model.Source) sources.Outcome {
		got = s
		return sources.Outcome{
			Matches: []model.Match{match("kill-port", "kill the process on a port")},
			Reached: []model.Source{model.CratesIO, model.Npm, model.PyPI, model.Homebrew},
			Cached:  []model.Source{model.Npm},
			Browsed: []model.Source{model.PyPI},
			ByName:  []model.Source{model.Homebrew},
		}
	}
	t.Cleanup(func() { searchFiltered = orig })
	var logs []string
	cfg := Config{KeywordOnly: true, Limit: 50, Cache: cache, Browser: nopBrowser{}, Catalogs: catalogs, PyPIIndex: true}
	if _, err := Run(context.Background(), "kill process port", cfg, func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	if got.Cache != cache || got.Browser != cfg.Browser || got.Catalogs != catalogs || !got.PyPIIndex {
		t.Errorf("the fan-out got %+v, want the caches and the browser", got)
	}
	if len(logs) < 2 || !strings.HasSuffix(logs[0], "s: crates.io, npm, PyPI (through CloakBrowser), Homebrew (names only)") ||
		logs[1] != "   from the local cache (less than 1h30m old): npm" {
		t.Errorf("logs = %q", logs)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Hour:                   "1h",
		90 * time.Minute:            "1h30m",
		30 * time.Minute:            "30m",
		10 * time.Second:            "10s",
		2*time.Minute + time.Second: "2m1s",
		24 * time.Hour:              "24h",
		time.Hour + 30*time.Second:  "1h0m30s",
		500 * time.Millisecond:      "500ms",
	} {
		if got := ShortDuration(d); got != want {
			t.Errorf("ShortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestMain(m *testing.M) {
	// No test may reach a real Ollama on this machine: a test that needs one
	// serves it with fakeOllama.
	ollamaEndpoint = "http://127.0.0.1:1"
	os.Exit(m.Run())
}

func ptr[T any](v T) *T { return &v }

// fakeOllama points the default LLM backend at a server running h for the
// duration of a test, and returns the server's URL.
func fakeOllama(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	orig := ollamaEndpoint
	ollamaEndpoint = srv.URL
	t.Cleanup(func() { ollamaEndpoint = orig })
	return srv.URL
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck // a test response
}

const modelVerdict = `{"level": "Crowded", "headline": "kill-port and fkill already do this.", "gaps": ["a Windows build"]}`

// killPortSearch finds two matches for "kill process port" that rank above
// the relevance gate.
func killPortSearch(t *testing.T) {
	fakeSearch(t, sources.Outcome{
		Matches: []model.Match{match("kill-port", "kill the process on a port"), match("fkill", "kill processes on a port")},
		Reached: []model.Source{model.Npm},
		Failed:  []model.Source{model.PyPI},
	})
}

func TestRunGeneratesTheLLMVerdictViaOllamaByDefault(t *testing.T) {
	killPortSearch(t)
	var req struct{ Model, Prompt string }
	endpoint := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		writeJSON(w, 200, map[string]any{"response": modelVerdict, "done": true})
	})
	var logs []string
	res, err := Run(context.Background(), "kill process port", Config{LLMVerdict: true, KeywordOnly: true, Limit: 1},
		func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "qwen3.5" || !strings.Contains(req.Prompt, "for existing implementations: npm.\n") ||
		!strings.Contains(req.Prompt, "**fkill**") {
		t.Errorf("request = %+v; want the default model and a prompt with all ranked matches and the reached sources", req)
	}
	want := model.Verdict{
		Level:          model.Crowded,
		Headline:       "kill-port and fkill already do this.",
		Gaps:           []string{"a Windows build"},
		SourcesChecked: []model.Source{model.Npm},
		SourcesFailed:  []model.Source{model.PyPI},
		Caveat:         verdict.Caveat,
	}
	if !reflect.DeepEqual(res.Verdict, want) || len(res.Matches) != 1 {
		t.Errorf("result = %+v", res)
	}
	if got := logs[len(logs)-2]; got != "Generating verdict via Ollama at "+endpoint+" (qwen3.5)..." {
		t.Errorf("log = %q", got)
	}
	if got := logs[len(logs)-1]; !strings.HasPrefix(got, "   verdict in ") {
		t.Errorf("log = %q", got)
	}
}

func TestRunAsksNoLLMUnlessTold(t *testing.T) {
	killPortSearch(t)
	fakeOllama(t, func(http.ResponseWriter, *http.Request) {
		t.Error("the LLM was asked without LLMVerdict")
	})
	res, err := Run(context.Background(), "kill process port", Config{KeywordOnly: true, Limit: 50, Model: ptr("qwen3.5")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Verdict{
		Level:          model.Crowded,
		Headline:       "We believe this space is crowded: 2 closely-related tools turned up in the sources checked.",
		Gaps:           []string{},
		SourcesChecked: []model.Source{model.Npm},
		SourcesFailed:  []model.Source{model.PyPI},
		Caveat:         verdict.Caveat,
	}
	if !reflect.DeepEqual(res.Verdict, want) {
		t.Errorf("verdict = %+v\nwant %+v", res.Verdict, want)
	}
}

func TestRunUsesTheConfiguredOllamaEndpoint(t *testing.T) {
	killPortSearch(t)
	// The default endpoint stays the dead one TestMain sets, so only a
	// request to the configured one can produce the verdict.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"response": modelVerdict, "done": true})
	}))
	t.Cleanup(srv.Close)
	res, err := Run(context.Background(), "kill process port", Config{LLMVerdict: true, KeywordOnly: true, Limit: 1, OllamaEndpoint: ptr(srv.URL)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Headline != "kill-port and fkill already do this." {
		t.Errorf("verdict = %+v", res.Verdict)
	}
}

func TestRunUsesTheOpenAICompatibleAPIWhenAPIBaseIsSet(t *testing.T) {
	killPortSearch(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("path %q, Authorization %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var req struct{ Model string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model != "gpt-4o-mini" {
			t.Errorf("model %q, %v", req.Model, err)
		}
		writeJSON(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": modelVerdict}}}})
	}))
	t.Cleanup(srv.Close)
	var logs []string
	cfg := Config{LLMVerdict: true, KeywordOnly: true, Limit: 50, APIBase: ptr(srv.URL + "/v1"), APIKey: ptr("sk-test"), Model: ptr("gpt-4o-mini")}
	res, err := Run(context.Background(), "kill process port", cfg, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Headline != "kill-port and fkill already do this." {
		t.Errorf("verdict = %+v", res.Verdict)
	}
	if got := logs[len(logs)-2]; got != "Generating verdict via OpenAI-compatible API at "+srv.URL+"/v1 (gpt-4o-mini)..." {
		t.Errorf("log = %q", got)
	}
}

func TestRunFallsBackToSearchOnlyVerdictWhenTheLLMFails(t *testing.T) {
	killPortSearch(t)
	endpoint := fakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 404, map[string]any{"error": "model 'qwen2.5:3b' not found"})
	})
	var logs []string
	res, err := Run(context.Background(), "kill process port", Config{LLMVerdict: true, KeywordOnly: true, Limit: 50, Model: ptr("qwen2.5:3b")},
		func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	want := model.Verdict{
		Level:          model.Open,
		Headline:       "Verdict unavailable — results are ranked by semantic similarity only.",
		Gaps:           []string{},
		SourcesChecked: []model.Source{model.Npm},
		SourcesFailed:  []model.Source{model.PyPI},
		Caveat:         verdict.Caveat,
	}
	if !reflect.DeepEqual(res.Verdict, want) || len(res.Matches) != 2 {
		t.Errorf("result = %+v", res)
	}
	wantLogs := []string{
		"Generating verdict via Ollama at " + endpoint + " (qwen2.5:3b)...",
		"warning: model 'qwen2.5:3b' not found (model `qwen2.5:3b`). Run `ollama pull qwen2.5:3b`.",
		"   showing results without an AI verdict.",
	}
	if got := logs[len(logs)-3:]; !reflect.DeepEqual(got, wantLogs) {
		t.Errorf("logs = %q\nwant %q", got, wantLogs)
	}
}

// fakeRanker scores a match by a similarity looked up by name.
type fakeRanker struct{ sims map[string]float32 }

func (fakeRanker) EmbedQuery(string) []float32 { return []float32{1} }

func (f fakeRanker) RankWith(_ []float32, matches []model.Match, limit int) []model.Match {
	for i := range matches {
		matches[i].Similarity = f.sims[matches[i].Name]
	}
	slices.SortStableFunc(matches, func(a, b model.Match) int { return cmp.Compare(b.Similarity, a.Similarity) })
	return matches[:min(limit, len(matches))]
}

// fakeModel replaces the model loader for the duration of a test.
func fakeModel(t *testing.T, load func(context.Context, rank.Progress) (ranker, error)) {
	t.Helper()
	orig := newRanker
	newRanker = load
	t.Cleanup(func() { newRanker = orig })
}

func TestRunRanksSemanticallyWithTheModel(t *testing.T) {
	fakeSearch(t, sources.Outcome{
		Matches: []model.Match{match("weak", "x"), match("close", "y"), match("closer", "z")},
		Reached: []model.Source{model.Npm},
	})
	fakeModel(t, func(context.Context, rank.Progress) (ranker, error) {
		return fakeRanker{map[string]float32{"weak": 0.2, "close": 0.6, "closer": 0.75}}, nil
	})
	var logs []string
	res, err := Run(context.Background(), "kill process port", Config{Limit: 2},
		func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 2 || res.Matches[0].Name != "closer" || res.Matches[1].Name != "close" {
		t.Errorf("matches = %+v", res.Matches)
	}
	// Two at >= 0.55, judged on all three before the limit applies.
	if res.Verdict.Level != model.Crowded {
		t.Errorf("level = %v", res.Verdict.Level)
	}
	if !strings.HasPrefix(logs[1], "Ranked to top 3 in ") {
		t.Errorf("logs = %q", logs)
	}
}

func TestRunFailsWhenTheModelCannotBeLoaded(t *testing.T) {
	fakeSearch(t, sources.Outcome{Matches: []model.Match{match("a", "b")}})
	fakeModel(t, func(context.Context, rank.Progress) (ranker, error) {
		return nil, &silt.EmbeddingError{Msg: "downloading model.onnx: no network"}
	})
	_, err := Run(context.Background(), "kill process port", Config{Limit: 50}, nil)
	var embedding *silt.EmbeddingError
	if !errors.As(err, &embedding) {
		t.Errorf("err = %v, want the EmbeddingError", err)
	}
}

func TestRunLoadsTheModelWhileTheSourcesSearch(t *testing.T) {
	// The search waits for the model load to begin, and the load waits for
	// the search to begin: run one after the other, they would deadlock.
	loading, searching := make(chan struct{}), make(chan struct{})
	orig := searchFiltered
	searchFiltered = func(context.Context, sources.Searcher, model.Query, []model.Source, []model.Source) sources.Outcome {
		close(searching)
		<-loading
		return sources.Outcome{}
	}
	t.Cleanup(func() { searchFiltered = orig })
	fakeModel(t, func(context.Context, rank.Progress) (ranker, error) {
		close(loading)
		<-searching
		return fakeRanker{}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), "kill process port", Config{Limit: 50}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the model load and the search did not overlap")
	}
}

func TestRunSaysWhichSourcesWereRateLimited(t *testing.T) {
	reset := time.Date(2026, 10, 29, 21, 21, 47, 0, time.Local)
	fakeSearch(t, sources.Outcome{
		Matches: []model.Match{match("kill-port", "kill the process on a port")},
		Reached: []model.Source{model.Npm},
		Failed:  []model.Source{model.CratesIO, model.GitHub},
		RateLimited: []sources.RateLimit{
			{Source: model.CratesIO},
			{Source: model.GitHub, Reset: reset, Hint: "set GITHUB_TOKEN for a higher limit"},
		},
	})
	var logs []string
	if _, err := Run(context.Background(), "kill process port", Config{KeywordOnly: true, Limit: 50},
		func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	want := []string{"   crates.io: rate-limited", "   GitHub: rate-limited until 21:22; set GITHUB_TOKEN for a higher limit"}
	if len(logs) < 3 || !reflect.DeepEqual(logs[1:3], want) {
		t.Errorf("logs = %q, want %q after the matches line", logs, want)
	}
}

func TestRunSaysWhichSourcesStoodInFromTheCache(t *testing.T) {
	fakeSearch(t, sources.Outcome{
		Matches: []model.Match{match("kill-port", "kill the process on a port")},
		Reached: []model.Source{model.Npm, model.CratesIO, model.GitHub},
		Stale: []sources.StaleResult{
			{Source: model.CratesIO, Age: 5*time.Hour + 12*time.Minute + 30*time.Second},
			{Source: model.GitHub, Age: 20 * time.Second},
		},
	})
	var logs []string
	if _, err := Run(context.Background(), "kill process port", Config{KeywordOnly: true, Limit: 50},
		func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	if want := "   not reached now, from the local cache instead: crates.io (5h12m old), GitHub (1m old)"; len(logs) < 2 || logs[1] != want {
		t.Errorf("logs = %q, want %q second", logs, want)
	}
}
