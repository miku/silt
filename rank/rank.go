// Package rank orders matches by how closely they relate to the idea.
package rank

import (
	"cmp"
	"slices"
	"strings"

	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/model"
)

// DefaultLimit is the default number of matches to keep after ranking.
const DefaultLimit = 50

// ByKeywords ranks matches by keyword overlap: each match is scored by the
// fraction of q's keywords found in its name or description, sorted by that
// score (stably, best first), and those scoring zero are dropped. At most
// limit matches are kept. It skips the embedding model entirely — no
// download, instant results.
//
// With no matches or no keywords there is nothing to score, so the matches
// come back unscored and in their original order.
func ByKeywords(q model.Query, matches []model.Match, limit int) []model.Match {
	if len(matches) == 0 || len(q.Keywords) == 0 {
		return matches[:min(limit, len(matches))]
	}
	keywords := make([]string, len(q.Keywords))
	for i, k := range q.Keywords {
		keywords[i] = textutil.ToLower(k)
	}
	for i := range matches {
		text := textutil.ToLower(matches[i].Name + " " + matches[i].Description)
		hits := 0
		for _, k := range keywords {
			if strings.Contains(text, k) {
				hits++
			}
		}
		matches[i].Similarity = float32(hits) / float32(len(keywords))
	}
	slices.SortStableFunc(matches, func(a, b model.Match) int {
		return cmp.Compare(b.Similarity, a.Similarity)
	})
	matches = slices.DeleteFunc(matches, func(m model.Match) bool { return m.Similarity <= 0 })
	return matches[:min(limit, len(matches))]
}
