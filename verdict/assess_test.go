package verdict

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/miku/silt"
	"github.com/miku/silt/llm/ollama"
	"github.com/miku/silt/model"
)

// -- Assess end to end, through the Ollama client and a mock server -----------

// ollamaResponse is Ollama's reply carrying the model's verdict JSON.
func ollamaResponse(level, headline string, gaps ...string) map[string]any {
	if gaps == nil {
		gaps = []string{}
	}
	verdict, _ := json.Marshal(map[string]any{"level": level, "headline": headline, "gaps": gaps})
	return map[string]any{"response": string(verdict), "done": true}
}

// mockOllama answers POST /api/generate with response; anything else is a
// 404, as an unmatched wiremock request is.
func mockOllama(t *testing.T, response map[string]any) *httptest.Server {
	t.Helper()
	return mockOllamaFunc(t, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, response) })
}

func mockOllamaFunc(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck // a test response
}

// assess runs Assess against Ollama at srv with the model qwen2.5.
func assess(t *testing.T, srv *httptest.Server, matches []model.Match, checked, failed []model.Source) (model.Verdict, error) {
	t.Helper()
	return Assess(t.Context(), ollama.New(srv.URL, "qwen2.5"), query(), matches, checked, failed)
}

func mustAssess(t *testing.T, srv *httptest.Server, matches []model.Match, checked, failed []model.Source) model.Verdict {
	t.Helper()
	v, err := assess(t, srv, matches, checked, failed)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAssessDropsGapsNamingATopMatch(t *testing.T) {
	// A gap that names a top-10 match by name should be filtered out — the
	// model naming "kill-port" in a gap is confirming it exists, not
	// identifying open space. A clean gap with no match name must survive.
	srv := mockOllama(t, ollamaResponse("Crowded", "Several tools exist in the sources checked.",
		"kill-port does not support IPv6",
		"none of the tools support Windows arm64"))
	v := mustAssess(t, srv, sampleMatches(), checked(), nil)
	if len(v.Gaps) != 1 || !strings.Contains(v.Gaps[0], "Windows arm64") {
		t.Errorf("gaps = %q: the gap naming a top match must be dropped, the clean one kept", v.Gaps)
	}
}

func TestAssessGapFilterUsesWordBoundaries(t *testing.T) {
	// "at" is a match name that appears as a *substring* inside
	// "automatically". The gap must survive — only whole-word occurrences
	// should be filtered.
	matches := []model.Match{{
		Name: "at", Source: model.Npm, URL: "https://npmjs.com/package/at",
		Description: "schedule tasks", Popularity: ptr(uint64(1000)), Similarity: 0.80,
	}}
	srv := mockOllama(t, ollamaResponse("Crowded", "Several tools exist in the sources checked.",
		"automatically handles port conflicts", // "at" is a substring here → must survive
		"at startup it reads config"))          // "at" as a whole word → must be dropped
	v := mustAssess(t, srv, matches, checked(), nil)
	if len(v.Gaps) != 1 || !strings.Contains(v.Gaps[0], "automatically") {
		t.Errorf("gaps = %q: only the whole-word 'at' gap should be dropped", v.Gaps)
	}
}

func TestAssessReturnsVerdictWithCaveat(t *testing.T) {
	srv := mockOllama(t, ollamaResponse("Saturated", "Lots of prior art found in the sources checked.",
		"no Windows support in existing tools"))
	v := mustAssess(t, srv, sampleMatches(), checked(), nil)
	if v.Level != model.Saturated || !strings.Contains(v.Headline, "prior art") || len(v.Gaps) != 1 ||
		!reflect.DeepEqual(v.SourcesChecked, checked()) || v.Caveat != Caveat {
		t.Errorf("verdict = %+v", v)
	}
}

func TestAssessParsesOpenLevel(t *testing.T) {
	srv := mockOllama(t, ollamaResponse("Open", "Nothing close found in the sources checked."))
	if v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, nil); v.Level != model.Open {
		t.Errorf("level = %v", v.Level)
	}
}

func TestAssessParsesCrowdedLevel(t *testing.T) {
	srv := mockOllama(t, ollamaResponse("Crowded", "A few adjacent tools exist.", "gap one", "gap two"))
	v := mustAssess(t, srv, sampleMatches(), []model.Source{model.Npm}, nil)
	if v.Level != model.Crowded || len(v.Gaps) != 2 {
		t.Errorf("verdict = %+v", v)
	}
}

func TestAssessHandlesJSONWrappedInMarkdownFence(t *testing.T) {
	fenced := "```json\n{\"level\":\"Open\",\"headline\":\"Nothing found.\",\"gaps\":[]}\n```"
	srv := mockOllama(t, map[string]any{"response": fenced, "done": true})
	if v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, nil); v.Level != model.Open {
		t.Errorf("level = %v", v.Level)
	}
}

func TestAssessSkipsAReasoningTraceThatDraftsItsOwnJSON(t *testing.T) {
	// A DeepSeek-R1-style inline trace that talks itself through a draft
	// verdict before answering. The draft says Saturated and the answer says
	// Open, so reading the trace instead of the answer is visible in the
	// result rather than merely a parse error.
	traced := "<think>Let me draft: ```json\n" +
		"{\"level\":\"Saturated\",\"headline\":\"Draft from the trace.\",\"gaps\":[]}\n" +
		"```\nNo, the search found nothing. Answer Open.</think>\n" +
		"{\"level\":\"Open\",\"headline\":\"Nothing found in the sources checked.\",\"gaps\":[]}"
	srv := mockOllama(t, map[string]any{"response": traced, "done": true})
	v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, nil)
	if v.Level != model.Open || !strings.Contains(v.Headline, "sources checked") {
		t.Errorf("verdict = %+v", v)
	}
}

func TestAssessAcceptsAClosingThinkTagWithNoOpener(t *testing.T) {
	// Some chat templates open the thinking block for the model, so the
	// response carries only the closing tag.
	traced := "Searching my memory of the ecosystem.</think>\n" +
		"{\"level\":\"Open\",\"headline\":\"Nothing found in the sources checked.\",\"gaps\":[]}"
	srv := mockOllama(t, map[string]any{"response": traced, "done": true})
	if v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, nil); v.Level != model.Open {
		t.Errorf("level = %v", v.Level)
	}
}

func TestAssessRejectsAnUnterminatedReasoningTrace(t *testing.T) {
	// Trace with no closing tag and no answer: nothing parseable was
	// returned, and inventing a verdict out of the trace body would be worse
	// than falling back.
	srv := mockOllama(t, map[string]any{"response": "<think>Let me consider the crates.io results", "done": true})
	if _, err := assess(t, srv, nil, nil, nil); !errors.As(err, new(*silt.ParseError)) {
		t.Errorf("err = %#v, want a ParseError", err)
	}
}

func TestAssessRejectsGarbageResponse(t *testing.T) {
	srv := mockOllama(t, map[string]any{"response": "I don't know what JSON is", "done": true})
	if _, err := assess(t, srv, nil, nil, nil); !errors.As(err, new(*silt.ParseError)) {
		t.Errorf("err = %#v, want a ParseError", err)
	}
}

func TestAssessFloorsLevelWhenModelUnderratesACrowdedSpace(t *testing.T) {
	// The model says Open but the similarity data shows two close matches
	// (0.85, 0.60). The level must be floored up to Crowded, and because the
	// model misjudged, its headline is replaced with a safe data-derived one.
	srv := mockOllama(t, ollamaResponse("Open", "This is a brand-new idea about a testing framework."))
	v := mustAssess(t, srv, sampleMatches(), checked(), nil)
	if v.Level != model.Crowded {
		t.Errorf("level = %v: two >= 0.55 matches => Crowded", v.Level)
	}
	if strings.Contains(v.Headline, "testing framework") || !strings.Contains(strings.ToLower(v.Headline), "sources checked") {
		t.Errorf("a floored level must not keep the model's misjudged headline: %q", v.Headline)
	}
}

func TestAssessReplacesAbsenceClaimingHeadline(t *testing.T) {
	// No matches => the level stays Open, so the model's headline is kept
	// *unless* it asserts non-existence — which it does here, and must be
	// replaced.
	srv := mockOllama(t, ollamaResponse("Open", "This tool does not exist and there is no prior art for it."))
	v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, nil)
	lower := strings.ToLower(v.Headline)
	if strings.Contains(lower, "does not exist") || strings.Contains(lower, "no prior art") {
		t.Errorf("absence claim must be scrubbed: %q", v.Headline)
	}
	if v.Caveat != Caveat {
		t.Errorf("caveat = %q", v.Caveat)
	}
}

func TestAssessDropsGapsThatAssertAbsence(t *testing.T) {
	// A gap bullet that smuggles an absence claim must be filtered out; a
	// legitimate gap is kept.
	srv := mockOllama(t, ollamaResponse("Open", "A few adjacent tools turned up in the sources checked.",
		"no existing tool supports Windows, and there is no prior art for this",
		"none of the matches offer an async API"))
	v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, nil)
	if len(v.Gaps) != 1 || !strings.Contains(v.Gaps[0], "async API") {
		t.Errorf("gaps = %q: the absence-asserting gap must be dropped", v.Gaps)
	}
}

func TestAssessScrubsBroadenedAbsencePhrasings(t *testing.T) {
	// Phrasings beyond the obvious ones must also be caught.
	for _, headline := range []string{
		"This has not been implemented anywhere yet.",
		"This is unprecedented — no similar tool exists.",
		"There is no existing software like this.",
	} {
		srv := mockOllama(t, ollamaResponse("Open", headline))
		v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, nil)
		lower := strings.ToLower(v.Headline)
		for _, claim := range []string{"has not been implemented", "unprecedented", "no similar tool", "no existing software"} {
			if strings.Contains(lower, claim) {
				t.Errorf("absence headline survived: %q", v.Headline)
			}
		}
	}
}

func TestAssessScrubsAbsenceClaimsDressedUpAsMaintenance(t *testing.T) {
	// #29 opened a second door to the one claim this tool must never make.
	// With dates in the prompt, "nothing exists" becomes "nothing
	// *maintained* exists" — which is just as unprovable (only 9 of 19
	// sources publish a date, so an undated match is not an unmaintained one)
	// and reads just as much like a green light. sampleMatches floors to
	// Crowded, which is what the model returns here, so the headline survives
	// the floor and it is genuinely guardHeadline doing the scrubbing.
	for _, headline := range []string{
		"No actively maintained tool solves this in the sources checked.",
		"No maintained alternative turned up.",
		"Nothing maintained covers this idea.",
		"There is no currently maintained tool for this.",
		"No actively developed option covers this.",
		"No up-to-date implementation of this exists.",
	} {
		srv := mockOllama(t, ollamaResponse("Crowded", headline))
		v := mustAssess(t, srv, sampleMatches(), checked(), nil)
		if v.Headline == headline {
			t.Errorf("maintenance-flavoured absence claim survived: %q", v.Headline)
		}
		if !strings.Contains(strings.ToLower(v.Headline), "sources checked") {
			t.Errorf("the replacement must be the scoped data headline, got %q", v.Headline)
		}
	}
}

func TestAssessDropsGapsClaimingNothingMaintainedExists(t *testing.T) {
	srv := mockOllama(t, ollamaResponse("Crowded", "Several tools turned up in the sources checked.",
		"no actively maintained tool offers a TUI",
		"none of the matches offer an async API"))
	v := mustAssess(t, srv, sampleMatches(), checked(), nil)
	if len(v.Gaps) != 1 || !strings.Contains(v.Gaps[0], "async API") {
		t.Errorf("gaps = %q: the gap asserting nothing maintained exists must be dropped", v.Gaps)
	}
}

func TestAssessKeepsAnHonestNoteThatAMatchIsUnmaintained(t *testing.T) {
	// The other side of the guard above, and the whole point of the feature:
	// saying a *specific* match looks abandoned is read straight off the
	// data we showed the user. Over-scrubbing that would throw away the
	// signal #29 exists to surface, so this pins the boundary.
	honest := "Crowded — fkill-cli covers this, though it is no longer maintained."
	srv := mockOllama(t, ollamaResponse("Crowded", honest))
	if v := mustAssess(t, srv, sampleMatches(), checked(), nil); v.Headline != honest {
		t.Errorf("a per-match maintenance note is colour, not an absence claim: %q", v.Headline)
	}
}

func TestAssessThreadsFailedSourcesIntoVerdict(t *testing.T) {
	srv := mockOllama(t, ollamaResponse("Open", "Nothing close turned up."))
	failed := []model.Source{model.PyPI, model.CratesIO}
	if v := mustAssess(t, srv, nil, []model.Source{model.GitHub}, failed); !reflect.DeepEqual(v.SourcesFailed, failed) {
		t.Errorf("sources_failed = %v", v.SourcesFailed)
	}
}

func TestAssessReplacesNoMatchHeadlineWhenACloseMatchExists(t *testing.T) {
	// A single 0.57 match keeps the level Open, but a "no direct matches"
	// headline would be misleading when that match is real prior art. It
	// must be replaced with one that names the close match.
	matches := []model.Match{{
		Name: "patent", Source: model.CratesIO, URL: "https://crates.io/crates/patent",
		Description: "A prior-art search for your code ideas", Similarity: 0.57,
	}}
	srv := mockOllama(t, ollamaResponse("Open", "No direct matches found in the sources checked."))
	v := mustAssess(t, srv, matches, checked(), nil)
	if v.Level != model.Open {
		t.Errorf("level = %v: one 0.57 match stays Open", v.Level)
	}
	lower := strings.ToLower(v.Headline)
	if strings.Contains(lower, "no direct match") || !strings.Contains(lower, "closely-related") {
		t.Errorf("the 'no match' headline must be replaced by one naming the close match: %q", v.Headline)
	}
}

func TestAssessFloorsSingleVeryStrongMatchToCrowded(t *testing.T) {
	// A lone near-identical match (>= 0.70) must lift the level off Open
	// even though only one match exists (the close >= 2 path does not apply
	// here).
	matches := []model.Match{{
		Name: "twin", Source: model.CratesIO, URL: "https://crates.io/crates/twin",
		Description: "Nearly the same idea", Similarity: 0.72,
	}}
	srv := mockOllama(t, ollamaResponse("Open", "A benign headline."))
	v := mustAssess(t, srv, matches, checked(), nil)
	if v.Level != model.Crowded || !strings.Contains(strings.ToLower(v.Headline), "closely-related") {
		t.Errorf("verdict = %+v: one 0.72 match => Crowded", v)
	}
}

func TestAssessRetriesOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := mockOllamaFunc(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(500)
			return
		}
		writeJSON(w, 200, ollamaResponse("Crowded", "Several tools exist in the sources checked."))
	})
	if v := mustAssess(t, srv, sampleMatches(), checked(), nil); v.Level != model.Crowded {
		t.Errorf("level = %v", v.Level)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("%d calls, want 2", n)
	}
}

// -- Assess with a stand-in backend -------------------------------------------

// fakeLLM answers from a list of replies, one per call, and counts the calls.
type fakeLLM struct {
	replies []reply
	calls   int
}

type reply struct {
	text string
	err  error
}

func (f *fakeLLM) Generate(ctx context.Context, _ string) (string, error) {
	r := f.replies[min(f.calls, len(f.replies)-1)]
	f.calls++
	return r.text, r.err
}

func (*fakeLLM) Label() string { return "fake" }

const okReply = `{"level": "Crowded", "headline": "Several tools.", "gaps": []}`

// noRetryDelay drops the pause before a retry for the duration of a test.
func noRetryDelay(t *testing.T) {
	orig := retryDelay
	retryDelay = 0
	t.Cleanup(func() { retryDelay = orig })
}

func TestAssessRetriesOnlyHTTPAndUnreachableErrors(t *testing.T) {
	noRetryDelay(t)
	for _, tc := range []struct {
		err   error
		retry bool
	}{
		{&silt.HTTPError{Err: errors.New("reset")}, true},
		{&silt.LLMUnreachableError{Msg: "down"}, true},
		{fmt.Errorf("wrapped: %w", &silt.LLMUnreachableError{Msg: "down"}), true},
		{&silt.LLMRejectedError{Msg: "no such model"}, false},
		{&silt.ParseError{Msg: "missing 'response' field"}, false},
	} {
		f := &fakeLLM{replies: []reply{{err: tc.err}, {text: okReply}}}
		_, err := Assess(t.Context(), f, query(), sampleMatches(), checked(), nil)
		if tc.retry && (err != nil || f.calls != 2) {
			t.Errorf("%v: %d calls, err %v; want a successful retry", tc.err, f.calls, err)
		}
		if !tc.retry && (!errors.Is(err, tc.err) || f.calls != 1) {
			t.Errorf("%v: %d calls, err %v; want no retry", tc.err, f.calls, err)
		}
	}
}

func TestAssessRetriesOnceAndReturnsTheSecondError(t *testing.T) {
	noRetryDelay(t)
	second := &silt.LLMRejectedError{Msg: "second"}
	f := &fakeLLM{replies: []reply{{err: &silt.LLMUnreachableError{Msg: "first"}}, {err: second}, {text: okReply}}}
	if _, err := Assess(t.Context(), f, query(), nil, nil, nil); !errors.Is(err, second) || f.calls != 2 {
		t.Errorf("%d calls, err %v", f.calls, err)
	}
}

func TestAssessStopsWaitingToRetryWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f := &fakeLLM{replies: []reply{{err: &silt.LLMUnreachableError{Msg: "down"}}, {text: okReply}}}
	if _, err := Assess(ctx, f, query(), nil, nil, nil); !errors.Is(err, context.Canceled) || f.calls != 1 {
		t.Errorf("%d calls, err %v", f.calls, err)
	}
}

func TestAssessSendsThePromptForTheReachedSources(t *testing.T) {
	var prompt string
	srv := mockOllamaFunc(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Prompt string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		prompt = body.Prompt
		writeJSON(w, 200, ollamaResponse("Crowded", "Several tools."))
	})
	mustAssess(t, srv, sampleMatches(), checked(), []model.Source{model.HackerNews})
	if prompt != BuildPrompt(query(), sampleMatches(), checked()) {
		t.Errorf("prompt = %q", prompt)
	}
}

// -- the guards ----------------------------------------------------------------

func TestExtractJSON(t *testing.T) {
	for raw, want := range map[string]string{
		`{"a":1}`:                       `{"a":1}`,
		" \n\u00a0{\"a\":1}\u3000\n":    `{"a":1}`,
		"```json\n{\"a\":1}\n```":       `{"a":1}`,
		"```\n{\"a\":1}\n```":           `{"a":1}`,
		"```json{\"a\":1}```":           `{"a":1}`,
		"Sure:\n```json\n{}\n```\nBye.": `{}`,
		// Only a lowercase tag is dropped.
		"```JSON\n{}\n```": "JSON\n{}",
		// An unclosed fence is left as it is, to fail to parse.
		"```json\n{}": "```json\n{}",
		// Everything up to the last closing tag is a trace.
		"<think>a</think>b</think> {} ":                                     "{}",
		"<think>a ```json\n{\"x\":1}\n```</think>\n```json\n{\"y\":2}\n```": `{"y":2}`,
		"<think>unterminated {}":                                            "<think>unterminated {}",
	} {
		if got := extractJSON(raw); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestIsWholeWord(t *testing.T) {
	for _, tc := range []struct {
		text, word string
		want       bool
	}{
		{"at startup", "at", true},
		{"automatically", "at", false},
		{"what at", "at", true},
		{"cat, bat, at.", "at", true},
		{"c# bindings", "c#", true},
		{"abc# bindings", "c#", false},
		{"node.js port", "node.js", true},
		{"größe", "gr", false}, // ö is alphanumeric
		{"ünïcode-tool", "ünïcode", true},
		{"x", "", false},
		{"", "x", false},
		// patent's quirk: the second "at" is checked against what is left
		// after the first, so the "t" before it goes unseen.
		{"atat", "at", true},
		{"atxat", "at", false},
	} {
		if got := isWholeWord(tc.text, tc.word); got != tc.want {
			t.Errorf("isWholeWord(%q, %q) = %v, want %v", tc.text, tc.word, got, tc.want)
		}
	}
}

func TestContainsAbsencePhraseStripsHedges(t *testing.T) {
	for text, want := range map[string]bool{
		"No really maintained tool exists.":       true,
		"There is no properly maintained option.": true,
		"NO MAINTAINED TOOL":                      true,
		"no developed tool":                       true, // the phrase "no actively developed", hedge stripped
		"fkill-cli is no longer maintained":       false,
		"several maintained tools":                false,
	} {
		if got := containsAbsencePhrase(text); got != want {
			t.Errorf("containsAbsencePhrase(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestParseVerdictErrors(t *testing.T) {
	for raw, want := range map[string]string{
		`{"level": "open", "headline": "x"}`:   `failed to parse response: invalid level: Some("open")`,
		`{"level": "", "headline": "x"}`:       `failed to parse response: invalid level: Some("")`,
		`{"level": 1, "headline": "x"}`:        `failed to parse response: invalid level: None`,
		`{"headline": "x"}`:                    `failed to parse response: invalid level: None`,
		`[]`:                                   `failed to parse response: invalid level: None`,
		`{"level": "Open"}`:                    `failed to parse response: missing 'headline'`,
		`{"level": "Open", "headline": null}`:  `failed to parse response: missing 'headline'`,
		`{"level": "Crowded", "headline": {}}`: `failed to parse response: missing 'headline'`,
	} {
		_, err := parseVerdict(raw, nil, nil, nil)
		if !errors.As(err, new(*silt.ParseError)) || err.Error() != want {
			t.Errorf("parseVerdict(%s) = %v\nwant %s", raw, err, want)
		}
	}
}

func TestParseVerdictMarshalsNoGapsAsAnEmptyList(t *testing.T) {
	v, err := parseVerdict(`{"level": "Crowded", "headline": "x", "gaps": "none"}`, sampleMatches(), nil, nil)
	if err != nil || v.Gaps == nil || len(v.Gaps) != 0 {
		t.Errorf("verdict = %+v, %v", v, err)
	}
}
