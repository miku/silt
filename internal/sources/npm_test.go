package sources

import (
	"context"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/miku/silt/internal/model"
)

const npmBody = `{
  "total": 2,
  "objects": [
    { "package": { "name": "express", "description": "Fast web framework.", "date": "2026-07-21T15:41:28.716Z" } },
    { "package": { "name": "koa", "description": "Next-gen web framework." } }
  ]
}`

func npmFor(srv *mockServer) *Npm { return &Npm{Client: NewHTTPClient(), BaseURL: srv.URL} }

func npmServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/-/v1/search", respond(status, body)))
}

func TestNpmIDIsNpm(t *testing.T) {
	if got := NewNpm(nil).ID(); got != model.Npm {
		t.Fatalf("ID() = %v", got)
	}
}

// npm answers 400 ERR_TEXT_LENGTH to a text over 64 characters, which took
// the whole source out for any idea written as a full sentence.
func TestNpmKeepsALongIdeaWithinTheTextLimit(t *testing.T) {
	srv := npmServer(t, 200, npmBody)
	idea := "fake OpenAI compatible API server returning deterministic responses for testing LLM applications in CI"
	long := model.Query{Idea: idea}
	for _, w := range strings.Fields(idea) {
		if len(w) > 2 {
			long.Keywords = append(long.Keywords, strings.ToLower(w))
		}
	}
	m, err := npmFor(srv).Search(context.Background(), long)
	if err != nil || len(m) != 2 {
		t.Fatalf("Search = %d matches, %v", len(m), err)
	}
	if n := utf8.RuneCountInString(srv.Requests()[0].URL.Query().Get("text")); n < 2 || n > 64 {
		t.Errorf("text has %d characters", n)
	}
}

func TestNpmSearchText(t *testing.T) {
	long := strings.Repeat("x", 70)
	for _, tc := range []struct {
		q    model.Query
		want string
	}{
		{model.Query{Idea: "a fast async runtime for rust", Keywords: []string{"fast", "async"}}, "a fast async runtime for rust"},
		// The most keywords that fit, longest first, in their own order.
		{model.Query{Idea: long, Keywords: []string{"aaaa", strings.Repeat("b", 40), strings.Repeat("c", 30)}}, strings.Repeat("b", 40)},
		{model.Query{Idea: long, Keywords: []string{"short", "words"}}, "short words"},
		// Nothing fits: the idea, cut to 64 characters.
		{model.Query{Idea: strings.Repeat("ü", 70)}, strings.Repeat("ü", 64)},
	} {
		if got := npmSearchText(tc.q); got != tc.want {
			t.Errorf("npmSearchText(%.30q) = %q, want %q", tc.q.Idea, got, tc.want)
		}
	}
}

func TestNpmMapsPackagesIntoMatches(t *testing.T) {
	m, err := npmFor(npmServer(t, 200, npmBody)).Search(context.Background(), query())
	if err != nil || len(m) != 2 {
		t.Fatalf("Search = %+v, %v", m, err)
	}
	first := m[0]
	// npm search exposes no integer download count.
	if first.Name != "express" || first.Source != model.Npm || first.URL != "https://www.npmjs.com/package/express" ||
		first.Description != "Fast web framework." || first.Popularity != nil || first.Similarity != 0 {
		t.Errorf("first = %+v", first)
	}
}

func TestNpmSendsTextQueryParam(t *testing.T) {
	srv := npmServer(t, 200, npmBody)
	if _, err := npmFor(srv).Search(context.Background(), query()); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].URL.Query().Get("text") != "a fast async runtime for rust" || reqs[0].URL.Query().Get("size") != "20" {
		t.Errorf("requests = %v", reqs[0].URL)
	}
}

func TestNpmMissingDescriptionBecomesEmpty(t *testing.T) {
	m, err := npmFor(npmServer(t, 200, `{"total": 1, "objects": [{"package": {"name": "bare"}}]}`)).Search(context.Background(), query())
	if err != nil || m[0].Description != "" {
		t.Errorf("Search = %+v, %v", m, err)
	}
}

func TestNpmEmptyResultsIsOK(t *testing.T) {
	m, err := npmFor(npmServer(t, 200, `{"total": 0, "objects": []}`)).Search(context.Background(), query())
	if err != nil || len(m) != 0 {
		t.Errorf("Search = %+v, %v", m, err)
	}
}

func TestNpmServerErrorIsPropagated(t *testing.T) {
	if _, err := npmFor(npmServer(t, 500, "")).Search(context.Background(), query()); err == nil {
		t.Error("a 5xx response must surface as an error")
	}
}

func TestNpmMalformedBodyIsPropagated(t *testing.T) {
	for _, body := range []string{"<html>not json</html>", `{"total": 0}`, `{"objects": [{"score": {}}]}`, `{"objects": [{"package": {}}]}`} {
		if _, err := npmFor(npmServer(t, 200, body)).Search(context.Background(), query()); err == nil {
			t.Errorf("%s: an unparseable body must surface as an error", body)
		}
	}
}

func TestNpmNormalisesMillisecondDates(t *testing.T) {
	m, err := npmFor(npmServer(t, 200, npmBody)).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if deref(m[0].LastUpdated) != "2026-07-21T15:41:28Z" || m[1].LastUpdated != nil {
		t.Errorf("dates = %v, %v", deref(m[0].LastUpdated), deref(m[1].LastUpdated))
	}
}

func TestNpmMalformedDateNeverFailsTheSource(t *testing.T) {
	body := strings.Replace(npmBody, `"2026-07-21T15:41:28.716Z"`, "1755000000", 1) // number, was a string
	m, err := npmFor(npmServer(t, 200, body)).Search(context.Background(), query())
	if err != nil || len(m) != 2 || m[0].LastUpdated != nil {
		t.Errorf("Search = %+v, %v", m, err)
	}
}

func TestNpmPopularityScalesTheScoreAsPatentDoes(t *testing.T) {
	body := `{"objects": [
		{"package": {"name": "a"}, "score": {"detail": {"popularity": 0.0213}}},
		{"package": {"name": "b"}, "score": {"detail": {"popularity": 0}}},
		{"package": {"name": "c"}, "score": {}},
		{"package": {"name": "d"}, "score": {"detail": {"popularity": -0.5}}},
		{"package": {"name": "e"}, "score": {"detail": {"popularity": 1e30}}}
	]}`
	m, err := npmFor(npmServer(t, 200, body)).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	// 0.0213 × 1e6 in float32 is 21300.0013..., cast down.
	want := []any{uint64(21300), nil, nil, nil, uint64(math.MaxUint64)}
	for i, w := range want {
		if got := deref(m[i].Popularity); got != w {
			t.Errorf("%s: popularity = %v, want %v", m[i].Name, got, w)
		}
	}
}
