package ollama

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/miku/silt"
)

// mockOllama serves h on POST /api/generate; anything else is a 404, as an
// unmatched wiremock request is.
func mockOllama(t *testing.T, h http.HandlerFunc) *httptest.Server {
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

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}
}

func generateResponse(text string) string {
	b, _ := json.Marshal(map[string]any{"response": text, "done": true})
	return string(b)
}

func TestGenerateReturnsResponseText(t *testing.T) {
	srv := mockOllama(t, respond(200, generateResponse("hello world")))
	got, err := New(srv.URL, "qwen2.5").Generate(t.Context(), "say hi")
	if err != nil || got != "hello world" {
		t.Errorf("Generate = %q, %v", got, err)
	}
}

func TestGenerateSendsModelAndPrompt(t *testing.T) {
	var (
		calls       int
		body        map[string]any
		contentType string
	)
	srv := mockOllama(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		respond(200, generateResponse("ok"))(w, r)
	})
	if _, err := New(srv.URL, "qwen2.5").Generate(t.Context(), "say hi"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"model":  "qwen2.5",
		"prompt": "say hi",
		"stream": false,
		"think":  false,
		"options": map[string]any{
			"temperature": 0.0,
			"num_predict": 512.0,
		},
	}
	if calls != 1 || !reflect.DeepEqual(body, want) || contentType != "application/json" {
		t.Errorf("%d calls, Content-Type %q, body %v", calls, contentType, body)
	}
}

func TestGenerateMapsConnectionErrorToLLMUnreachable(t *testing.T) {
	_, err := New("http://127.0.0.1:1", "qwen2.5").Generate(t.Context(), "hi")
	var unreachable *silt.LLMUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("expected LLMUnreachable, got: %#v", err)
	}
	if !strings.HasPrefix(err.Error(), "Ollama not reachable at http://127.0.0.1:1 (") ||
		!strings.HasSuffix(err.Error(), "). Run `ollama serve` and `ollama pull qwen2.5`.") {
		t.Errorf("err = %v", err)
	}
}

func TestGenerateMapsServerErrorToParse(t *testing.T) {
	srv := mockOllama(t, respond(200, "not json"))
	_, err := New(srv.URL, "qwen2.5").Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.ParseError)) {
		t.Errorf("expected Parse, got: %#v", err)
	}
}

func TestGenerateMapsModelNotFoundToLLMRejected(t *testing.T) {
	// Ollama is reachable but the model isn't pulled: it returns 404 with an
	// {"error": ...} body. This must be a recoverable LLMRejected error (so
	// the run degrades gracefully), not a fatal Parse error.
	srv := mockOllama(t, respond(404, `{"error": "model 'qwen2.5' not found, try pulling it first"}`))
	_, err := New(srv.URL, "qwen2.5").Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMRejectedError)) {
		t.Fatalf("expected LLMRejected, got: %#v", err)
	}
	if want := "model 'qwen2.5' not found, try pulling it first (model `qwen2.5`). Run `ollama pull qwen2.5`."; err.Error() != want {
		t.Errorf("err = %q\nwant %q", err, want)
	}
}

// The default model is a thinking model, so think: false is load-bearing:
// without it the reasoning trace consumes the whole num_predict budget and
// Ollama returns an empty response (verified live against qwen3.5).
func TestDefaultModelIsQwen35(t *testing.T) {
	if DefaultModel != "qwen3.5" {
		t.Errorf("DefaultModel = %q", DefaultModel)
	}
}

func TestGenerateMapsAFailedStatus(t *testing.T) {
	for _, tc := range []struct {
		status      int
		body        string
		unreachable bool
		want        string
	}{
		// A 5xx is the daemon or a proxy in front of it failing, which is
		// worth the retry Assess makes for unreachable backends.
		{500, "", true, "Ollama returned HTTP 500 (model `m`). Run `ollama pull m`."},
		{502, `{"error": "upstream down"}`, true, "upstream down (model `m`). Run `ollama pull m`."},
		{400, `{"error": {"message": "not a string"}}`, false, "Ollama returned HTTP 400 (model `m`). Run `ollama pull m`."},
		{403, "<html>forbidden</html>", false, "Ollama returned HTTP 403 (model `m`). Run `ollama pull m`."},
		// The message is lifted only from a body serde would accept whole.
		{404, `{"error": "x", "n": 1e400}`, false, "Ollama returned HTTP 404 (model `m`). Run `ollama pull m`."},
	} {
		srv := mockOllama(t, respond(tc.status, tc.body))
		_, err := New(srv.URL, "m").Generate(t.Context(), "hi")
		if got := errors.As(err, new(*silt.LLMUnreachableError)); got != tc.unreachable ||
			(!got && !errors.As(err, new(*silt.LLMRejectedError))) {
			t.Errorf("%d: err = %#v", tc.status, err)
		}
		if err == nil || err.Error() != tc.want {
			t.Errorf("%d: err = %v\nwant %s", tc.status, err, tc.want)
		}
	}
}

func TestGenerateMapsAnErrorIn200ToLLMRejected(t *testing.T) {
	// Some Ollama builds return 200 with an {"error": ...} body instead.
	srv := mockOllama(t, respond(200, `{"error": "model is loading", "response": "ignored"}`))
	_, err := New(srv.URL, "m").Generate(t.Context(), "hi")
	if !errors.As(err, new(*silt.LLMRejectedError)) || err.Error() != "model is loading (model `m`). Run `ollama pull m`." {
		t.Errorf("err = %#v", err)
	}
}

func TestGenerateMapsAMissingResponseToParse(t *testing.T) {
	for _, body := range []string{`{"done": true}`, `{"response": null}`, `{"response": 42}`, `[]`, `"text"`} {
		srv := mockOllama(t, respond(200, body))
		_, err := New(srv.URL, "m").Generate(t.Context(), "hi")
		if !errors.As(err, new(*silt.ParseError)) || err.Error() != "failed to parse response: missing 'response' field" {
			t.Errorf("%s: err = %#v", body, err)
		}
	}
}
