package sources

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/miku/silt/model"
)

const hnBody = `{
  "hits": [
    {
      "title": "Show HN: Tokio, an async runtime",
      "story_text": "We built an async runtime.",
      "url": "https://tokio.rs",
      "objectID": "12345",
      "points": 320
    },
    {
      "title": "Async Rust explained",
      "story_text": null,
      "url": null,
      "objectID": "67890",
      "points": 88
    }
  ]
}`

func hnFor(srv *mockServer) *HackerNews {
	return &HackerNews{Client: NewHTTPClient(), BaseURL: srv.URL}
}

func hnServer(t *testing.T, status int, body string) *mockServer {
	return newMockServer(t, onPath("/api/v1/search", respond(status, body)))
}

// hnStory serves a single hit with the given story_text.
func hnStory(t *testing.T, storyText string) *mockServer {
	body := `{"hits": [{"title": "Show HN: Something", "story_text": ` + storyText + `, "objectID": "99999", "points": 10}]}`
	return hnServer(t, 200, body)
}

func TestHNIDIsHackerNews(t *testing.T) {
	if got := NewHackerNews(nil).ID(); got != model.HackerNews {
		t.Fatalf("ID() = %v", got)
	}
}

func TestHNMapsHitsIntoMatches(t *testing.T) {
	srv := hnServer(t, 200, hnBody)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	first := matches[0]
	// The canonical HN URL is the discussion item, not the (optional)
	// outbound link.
	if first.Name != "Show HN: Tokio, an async runtime" || first.Source != model.HackerNews ||
		first.URL != "https://news.ycombinator.com/item?id=12345" ||
		first.Description != "We built an async runtime." ||
		deref(first.Popularity) != uint64(320) || first.Similarity != 0 || first.LastUpdated != nil {
		t.Errorf("first = %+v", first)
	}
}

func TestHNNullStoryTextFallsBackToTitle(t *testing.T) {
	srv := hnServer(t, 200, hnBody)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	m := matches[1]
	if m.Description != "Async Rust explained" || m.URL != "https://news.ycombinator.com/item?id=67890" ||
		deref(m.Popularity) != uint64(88) {
		t.Errorf("match = %+v", m)
	}
}

func TestHNSendsQueryParam(t *testing.T) {
	srv := hnServer(t, 200, hnBody)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil || len(matches) != 2 {
		t.Fatalf("Search = %+v, %v", matches, err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	params := reqs[0].URL.Query()
	if got := params.Get("query"); got != "a fast async runtime for rust" {
		t.Errorf("query = %q, want the idea verbatim", got)
	}
	if params.Get("hitsPerPage") != "20" || params.Get("tags") != "story" {
		t.Errorf("params = %v", params)
	}
}

func TestHNEmptyHitsIsOK(t *testing.T) {
	srv := hnServer(t, 200, `{"hits": []}`)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil || len(matches) != 0 {
		t.Errorf("Search = %+v, %v; want no matches", matches, err)
	}
}

func TestHNServerErrorIsPropagated(t *testing.T) {
	srv := hnServer(t, 500, "")
	if _, err := hnFor(srv).Search(context.Background(), query()); err == nil {
		t.Error("a 5xx response must surface as an error")
	}
}

func TestHNMalformedBodyIsPropagated(t *testing.T) {
	for _, body := range []string{"<html>not json</html>", `{}`, `{"hits": [{"title": "no id"}]}`} {
		srv := hnServer(t, 200, body)
		if _, err := hnFor(srv).Search(context.Background(), query()); err == nil {
			t.Errorf("%s: an unparseable body must surface as an error", body)
		}
	}
}

func TestHNStripsHTMLFromStoryText(t *testing.T) {
	srv := hnStory(t, `"<p>A tool that <b>does</b> things.</p>"`)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if got := matches[0].Description; got != "A tool that does things." {
		t.Errorf("description = %q", got)
	}
}

func TestHNBareAmpersandDoesNotEatRemainingText(t *testing.T) {
	// A bare '&' with no closing ';' must be emitted as a literal and must
	// not consume the text that follows it.
	srv := hnStory(t, `"fast tool for C & C++"`)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if got := matches[0].Description; got != "fast tool for C & C++" {
		t.Errorf("text after bare '&' must not be eaten: %q", got)
	}
}

func TestHNEmptyStoryTextFallsBackToTitle(t *testing.T) {
	srv := hnStory(t, `"<p> </p>"`)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if got := matches[0].Description; got != "Show HN: Something" {
		t.Errorf("description = %q", got)
	}
}

func TestHNTruncatesLongDescriptions(t *testing.T) {
	long := strings.Repeat("ü", 130)
	srv := hnStory(t, `"`+long+`"`)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("ü", 120) + "…"; matches[0].Description != want {
		t.Errorf("description = %q", matches[0].Description)
	}
}

func TestHNSkipsHitsWithoutATitle(t *testing.T) {
	srv := hnServer(t, 200, `{"hits": [{"title": null, "objectID": "1"}, {"title": "", "objectID": "2"}, {"objectID": "3", "title": "kept"}]}`)
	matches, err := hnFor(srv).Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Name != "kept" {
		t.Errorf("matches = %+v", matches)
	}
}

func TestHackerNewsNarrowsWhenTheIdeaAndFullQueryAreEmpty(t *testing.T) {
	// HN searches the raw idea first — a phrase match is the best answer when
	// it lands — then the keyword set, then the narrowings.
	srv := newMockServer(t, onPath("/api/v1/search", byParam("query", map[string]http.Handler{
		"an ide plugin that highlights spelling mistakes in code comments": respond(200, `{"hits": []}`),
		"ide code spell syntax mistakes":                                   respond(200, `{"hits": []}`),
		"spell syntax mistakes":                                            respond(200, hnBody),
	})))
	matches, err := hnFor(srv).Search(context.Background(), narrowingQuery())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("got %d matches, want 2", len(matches))
	}
	var got []string
	for _, r := range srv.Requests() {
		got = append(got, r.URL.Query().Get("query"))
	}
	want := []string{
		"an ide plugin that highlights spelling mistakes in code comments",
		"ide code spell syntax mistakes",
		"spell syntax mistakes",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("queries = %q, want %q", got, want)
	}
}

func TestDecodeHTMLEntities(t *testing.T) {
	for in, want := range map[string]string{
		"a &amp; b":            "a & b",
		"&lt;tag&gt;":          "<tag>",
		"&quot;q&quot; &apos;": `"q" '`,
		"&#65;&#x42;&#X43;":    "ABC",
		"&#xD800;":             "\uFFFD", // a surrogate is not a scalar value
		"&#x110000;":           "\uFFFD",
		"&#xZZ; &#; &#x;":      "  ",     // unparseable references are dropped
		"&nbsp;":               "&nbsp;", // unknown entities are kept as written
		"C & C++":              "C & C++",
		"&&amp;":               "&&amp;",
		"trailing &amp":        "trailing &amp",
		"&abcdefghijkl;":       "&abcdefghijkl;", // over 10 bytes: not an entity
		"&é;":                  "&é;",
	} {
		if got := decodeHTMLEntities(in); got != want {
			t.Errorf("decodeHTMLEntities(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripHTMLTagsDropsStrayClosingBrackets(t *testing.T) {
	// Ported as-is: patent drops every '>', even outside a tag.
	if got := stripHTMLTags("a > b"); got != "a  b" {
		t.Errorf("stripHTMLTags = %q", got)
	}
}
