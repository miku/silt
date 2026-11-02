package sources

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/miku/silt/internal/model"
	"github.com/miku/silt/internal/textutil"
)

const (
	hackerNewsBaseURL = "https://hn.algolia.com"
	maxHNDescLen      = 120
)

// HackerNews searches Hacker News via the free Algolia API:
// GET /api/v1/search?query=.
type HackerNews struct {
	Client  *http.Client
	BaseURL string
}

// NewHackerNews returns an adapter for the live HN Algolia API.
func NewHackerNews(client *http.Client) *HackerNews {
	return &HackerNews{Client: client, BaseURL: hackerNewsBaseURL}
}

// ID implements Adapter.
func (*HackerNews) ID() model.Source { return model.HackerNews }

// Search implements Adapter.
func (a *HackerNews) Search(ctx context.Context, q model.Query) ([]model.Match, error) {
	// Algolia ANDs every word, so a whole idea sentence reliably matches no
	// story even when one exists — measured live, the full idea returns zero
	// while its 2 longest keywords return plenty. Try the idea first (a
	// phrase match is the best result when it lands), then narrow.
	candidates := []string{q.Idea}
	for _, c := range narrowingCandidates(q, 2) {
		if !slices.Contains(candidates, c) {
			candidates = append(candidates, c)
		}
	}
	var matches []model.Match
	for _, term := range candidates {
		var body struct {
			Hits *[]struct {
				Title     string  `json:"title"`
				StoryText *string `json:"story_text"`
				ObjectID  *string `json:"objectID"`
				Points    *uint64 `json:"points"`
			} `json:"hits"`
		}
		params := queryParams{{"query", term}, {"hitsPerPage", "20"}, {"tags", "story"}}
		if err := getJSON(ctx, a.Client, a.BaseURL+"/api/v1/search", params, &body); err != nil {
			return nil, err
		}
		if body.Hits == nil {
			return nil, missingField("hits")
		}
		matches = matches[:0]
		for _, h := range *body.Hits {
			if h.ObjectID == nil {
				return nil, missingField("objectID")
			}
			if h.Title == "" {
				continue
			}
			desc := h.Title
			if h.StoryText != nil {
				if s := stripHTMLTags(*h.StoryText); s != "" {
					desc = s
				}
			}
			matches = append(matches, model.Match{
				Name:   h.Title,
				Source: model.HackerNews,
				// The canonical HN URL is the discussion item, not the
				// (optional) outbound link.
				URL:         "https://news.ycombinator.com/item?id=" + *h.ObjectID,
				Description: truncate(desc, maxHNDescLen),
				Popularity:  h.Points,
			})
		}
		if len(matches) > 0 {
			break
		}
	}
	return matches, nil
}

// stripHTMLTags drops everything between '<' and '>' (and the brackets
// themselves, even unpaired), trims, and decodes HTML entities.
func stripHTMLTags(html string) string {
	var b strings.Builder
	inTag := false
	for _, c := range html {
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
		case !inTag:
			b.WriteRune(c)
		}
	}
	return decodeHTMLEntities(strings.TrimSpace(b.String()))
}

// decodeHTMLEntities decodes the five XML entities and numeric character
// references. An unknown entity is kept as written; a '&' that never reaches
// a ';' is kept as a literal without swallowing the text after it.
func decodeHTMLEntities(s string) string {
	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '&' {
			out.WriteRune(runes[i])
			continue
		}
		var entity strings.Builder
		terminated := false
		for i+1 < len(runes) {
			i++
			ec := runes[i]
			if ec == ';' {
				terminated = true
				break
			}
			entity.WriteRune(ec)
			if entity.Len() > 10 || (!textutil.IsAlphanumeric(ec) && ec != '#') {
				break
			}
		}
		e := entity.String()
		if !terminated {
			out.WriteString("&" + e)
			continue
		}
		switch {
		case e == "amp":
			out.WriteByte('&')
		case e == "lt":
			out.WriteByte('<')
		case e == "gt":
			out.WriteByte('>')
		case e == "quot":
			out.WriteByte('"')
		case e == "apos":
			out.WriteByte('\'')
		case strings.HasPrefix(e, "#x") || strings.HasPrefix(e, "#X"):
			if cp, err := strconv.ParseUint(e[2:], 16, 32); err == nil {
				out.WriteRune(toRune(cp))
			}
		case strings.HasPrefix(e, "#"):
			if cp, err := strconv.ParseUint(e[1:], 10, 32); err == nil {
				out.WriteRune(toRune(cp))
			}
		default:
			out.WriteString("&" + e + ";")
		}
	}
	return out.String()
}

// toRune converts a code point to a rune, or to U+FFFD if it isn't a Unicode
// scalar value.
func toRune(cp uint64) rune {
	if r := rune(cp); cp <= utf8.MaxRune && utf8.ValidRune(r) {
		return r
	}
	return utf8.RuneError
}

// truncate shortens s to n characters, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
