package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miku/silt"
)

func ptr[T any](v T) *T { return &v }

// route answers a request whose JSON body equals body with status and
// response, as a wiremock mock with a body_json_string matcher does.
type route struct {
	body     map[string]any
	status   int
	response string
	calls    int
}

// mockAPI serves POST /chat/completions from routes, picking the first route
// whose body matches, or any request if a route's body is nil. Anything else
// is a 404, as an unmatched wiremock request is.
func mockAPI(t *testing.T, routes ...*route) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var (
		mu       sync.Mutex
		requests []*http.Request
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Method == http.MethodPost && r.URL.Path == "/chat/completions" {
			for _, rt := range routes {
				if rt.body == nil || reflect.DeepEqual(rt.body, body) {
					rt.calls++
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(rt.status)
					w.Write([]byte(rt.response))
					return
				}
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func chatResponse(content string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	return string(b)
}

// firstBody and retryBody are the request bodies sent for "say hi": the first
// attempt and the retry for a reasoning model.
func firstBody(model string) map[string]any {
	return map[string]any{
		"model":       model,
		"messages":    []any{map[string]any{"role": "user", "content": "say hi"}},
		"temperature": 0.0,
		"max_tokens":  2048.0,
	}
}

func retryBody(model string) map[string]any {
	return map[string]any{
		"model":                 model,
		"messages":              []any{map[string]any{"role": "user", "content": "say hi"}},
		"max_completion_tokens": 2048.0,
	}
}

func TestGenerateReturnsMessageContent(t *testing.T) {
	srv, _ := mockAPI(t, &route{status: 200, response: chatResponse("hi there")})
	got, err := New(srv.URL, "gpt-4o-mini", nil).Generate(t.Context(), "say hi")
	if err != nil || got != "hi there" {
		t.Errorf("Generate = %q, %v", got, err)
	}
}

func TestGenerateSendsModelAndUserMessage(t *testing.T) {
	rt := &route{body: firstBody("gpt-4o-mini"), status: 200, response: chatResponse("ok")}
	srv, requests := mockAPI(t, rt)
	if _, err := New(srv.URL, "gpt-4o-mini", nil).Generate(t.Context(), "say hi"); err != nil {
		t.Fatal(err)
	}
	if rt.calls != 1 {
		t.Errorf("%d calls", rt.calls)
	}
	r := (*requests)[0]
	if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "" {
		t.Errorf("headers = %v", r.Header)
	}
}

func TestGenerateSendsBearerAuthWhenKeySet(t *testing.T) {
	srv, requests := mockAPI(t, &route{status: 200, response: chatResponse("ok")})
	if _, err := New(srv.URL, "gpt-4o-mini", ptr("sk-test")).Generate(t.Context(), "hi"); err != nil {
		t.Fatal(err)
	}
	if got := (*requests)[0].Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestGenerateMapsConnectionErrorToLLMUnreachable(t *testing.T) {
	_, err := New("http://127.0.0.1:1", "gpt-4o-mini", nil).Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMUnreachableError)) {
		t.Fatalf("expected LLMUnreachable, got: %#v", err)
	}
	if !strings.HasPrefix(err.Error(), "OpenAI-compatible API at http://127.0.0.1:1 not reachable (") ||
		!strings.HasSuffix(err.Error(), "). Check --api-base.") {
		t.Errorf("err = %v", err)
	}
}

func TestGenerateMapsHTTPErrorToLLMRejected(t *testing.T) {
	srv, _ := mockAPI(t, &route{status: 401, response: `{"error": {"message": "Incorrect API key provided"}}`})
	_, err := New(srv.URL, "gpt-4o-mini", nil).Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMRejectedError)) {
		t.Fatalf("expected LLMRejected, got: %#v", err)
	}
	if want := "Incorrect API key provided (model `gpt-4o-mini`). Check --model and --api-key."; err.Error() != want {
		t.Errorf("err = %q\nwant %q", err, want)
	}
}

func TestGenerateMapsEmptyChoicesToLLMRejected(t *testing.T) {
	// A 200 with no usable content must degrade (recoverable), not abort the
	// run.
	srv, _ := mockAPI(t, &route{status: 200, response: `{"choices": []}`})
	_, err := New(srv.URL, "gpt-4o-mini", nil).Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMRejectedError)) || err.Error() != "API returned no message content (model `gpt-4o-mini`)." {
		t.Errorf("err = %#v", err)
	}
}

func TestGenerateMapsNonJSONSuccessBodyToLLMRejected(t *testing.T) {
	// A proxy returning a 200 HTML page must degrade, not abort.
	srv, _ := mockAPI(t, &route{status: 200, response: "<html>proxy</html>"})
	_, err := New(srv.URL, "gpt-4o-mini", nil).Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMRejectedError)) {
		t.Fatalf("expected LLMRejected, got: %#v", err)
	}
	if want := "API at " + srv.URL + " returned a non-JSON response (model `gpt-4o-mini`)."; err.Error() != want {
		t.Errorf("err = %q\nwant %q", err, want)
	}
}

func TestGenerateRetriesReasoningModelsWithMaxCompletionTokens(t *testing.T) {
	// OpenAI's o-series and GPT-5 reject max_tokens and a non-default
	// temperature. The retry must drop both and use the newer spelling.
	first := &route{body: firstBody("o3"), status: 400, response: `{"error": {"message": ` +
		`"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`}
	retry := &route{body: retryBody("o3"), status: 200, response: chatResponse("reasoned")}
	srv, _ := mockAPI(t, first, retry)
	got, err := New(srv.URL, "o3", nil).Generate(t.Context(), "say hi")
	if err != nil || got != "reasoned" {
		t.Errorf("Generate = %q, %v", got, err)
	}
	if first.calls != 1 || retry.calls != 1 {
		t.Errorf("calls: first %d, retry %d", first.calls, retry.calls)
	}
}

func TestGenerateRetriesWhenTheServerRejectsOnlyTemperature(t *testing.T) {
	first := &route{body: firstBody("o3"), status: 400, response: `{"error": {"message": ` +
		`"Unsupported value: 'temperature' does not support 0.0 with this model. Only the default (1) value is supported."}}`}
	retry := &route{body: retryBody("o3"), status: 200, response: chatResponse("reasoned")}
	srv, _ := mockAPI(t, first, retry)
	got, err := New(srv.URL, "o3", nil).Generate(t.Context(), "say hi")
	if err != nil || got != "reasoned" {
		t.Errorf("Generate = %q, %v", got, err)
	}
	if first.calls != 1 || retry.calls != 1 {
		t.Errorf("calls: first %d, retry %d", first.calls, retry.calls)
	}
}

func TestGenerateDoesNotRetryAnUnrelatedBadRequest(t *testing.T) {
	// An ordinary 400 (bad model name, malformed request) must degrade on the
	// first response -- the retry is for one documented incompatibility
	// only.
	rt := &route{status: 400, response: `{"error": {"message": "The model ` + "`nope`" + ` does not exist"}}`}
	srv, _ := mockAPI(t, rt)
	_, err := New(srv.URL, "nope", nil).Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMRejectedError)) || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %#v", err)
	}
	if rt.calls != 1 {
		t.Errorf("%d calls, want 1", rt.calls)
	}
}

func TestGenerateTrimsTrailingSlashesFromTheBaseURL(t *testing.T) {
	srv, requests := mockAPI(t, &route{status: 200, response: chatResponse("ok")})
	if _, err := New(srv.URL+"//", "m", nil).Generate(t.Context(), "hi"); err != nil {
		t.Fatal(err)
	}
	if got := (*requests)[0].URL.Path; got != "/chat/completions" {
		t.Errorf("path = %q", got)
	}
}

func TestGenerateMapsAServerErrorToLLMUnreachable(t *testing.T) {
	srv, _ := mockAPI(t, &route{status: 503, response: "overloaded"})
	_, err := New(srv.URL, "m", nil).Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMUnreachableError)) || err.Error() != "HTTP 503 (model `m`). Check --model and --api-key." {
		t.Errorf("err = %#v", err)
	}
}

func TestRejectsOurParameters(t *testing.T) {
	for body, want := range map[string]bool{
		`{"error": {"message": "Use 'max_completion_tokens' instead."}}`:         true,
		`{"error": {"message": "Unsupported value: 'temperature' must be 1."}}`:  true,
		`{"error": {"message": "temperature out of range"}}`:                     false,
		`{"error": {"message": "unsupported model"}}`:                            false,
		`{"error": "TEMPERATURE is UNSUPPORTED"}`:                                true, // no message: the whole body
		`plain text: max_completion_tokens`:                                      true,
		`{"error": {"message": "bad request"}, "hint": "max_completion_tokens"}`: false,
		// Lowercased like Rust's to_lowercase, where İ becomes i plus a
		// combining dot, so this does not spell max_completion_tokens.
		`{"error": {"message": "MAX_COMPLETİON_TOKENS"}}`: false,
	} {
		if got := rejectsOurParameters([]byte(body)); got != want {
			t.Errorf("rejectsOurParameters(%s) = %v, want %v", body, got, want)
		}
	}
}

func TestStrFollowsAPathLikeSerdeJSONIndexing(t *testing.T) {
	v := decode([]byte(`{"choices": [{"message": {"content": "x"}}, {"message": {"content": null}}], "error": "flat"}`))
	for _, tc := range []struct {
		path []any
		want string
		ok   bool
	}{
		{[]any{"choices", 0, "message", "content"}, "x", true},
		{[]any{"choices", 1, "message", "content"}, "", false},
		{[]any{"choices", 2, "message", "content"}, "", false},
		{[]any{"error", "message"}, "", false},
		{[]any{"error"}, "flat", true},
		{[]any{"choices", "0"}, "", false},
	} {
		if got, ok := str(v, tc.path...); got != tc.want || ok != tc.ok {
			t.Errorf("str(%v) = %q, %v", tc.path, got, ok)
		}
	}
}
