// Package verdict estimates how crowded the space of an idea is, from how
// similar the matches found are to it.
//
// The level counts the matches that come close to the idea:
//
//	Saturated  5 or more matches with a similarity of at least 0.60
//	Crowded    2 or more with at least 0.55, or 1 with at least 0.70
//	Open       anything less
//
// The thresholds are patent's, made for the cosine similarities of
// all-MiniLM-L6-v2. With --keyword-only, the same thresholds apply to the
// share of the idea's keywords a match contains.
//
// silt can prove that something exists, but never that it doesn't: it only
// searched some sources. So the headline is scoped to the sources checked,
// words the level as an estimate ("We believe this space is crowded: ..."),
// and never claims that nothing exists, and every verdict carries [Caveat].
package verdict

import (
	"fmt"
	"strings"

	"github.com/miku/silt/internal/model"
)

// The similarity thresholds of the level. The TUI colours the scores by them
// too, so that the table shows which matches the headline counts.
const (
	// Close is the similarity of a closely-related match: two of them make
	// a space crowded, and the headline counts them.
	Close float32 = 0.55
	// Strong is the similarity five matches need to make a space saturated.
	Strong float32 = 0.60
	// VeryStrong is the similarity at which a single match makes a space
	// crowded.
	VeryStrong float32 = 0.70
)

// Caveat is the fixed, humble caveat that every verdict carries into the JSON
// output; the TUI leaves it out. Never weaken this.
const Caveat = "Not proof it doesn't exist — only that nothing close turned up " +
	"in the sources checked. Keep looking (web, app stores, niche communities) before committing."

// FromData is the verdict on matches, scored by their similarity to the idea.
// checked are the sources that were searched; failed are those that were
// selected but not reached, which the verdict names so that a thin result is
// not mistaken for an empty space.
func FromData(matches []model.Match, checked, failed []model.Source) model.Verdict {
	level := levelOf(matches)
	return model.Verdict{
		Level:          level,
		Headline:       headline(level, countAtLeast(matches, Close)),
		SourcesChecked: checked,
		SourcesFailed:  failed,
		Caveat:         Caveat,
	}
}

// levelOf applies the rule in the package doc.
func levelOf(matches []model.Match) model.Saturation {
	switch {
	case countAtLeast(matches, Strong) >= 5:
		return model.Saturated
	case countAtLeast(matches, Close) >= 2 || countAtLeast(matches, VeryStrong) >= 1:
		return model.Crowded
	}
	return model.Open
}

// countAtLeast counts the matches with a similarity of at least min.
func countAtLeast(matches []model.Match, min float32) int {
	n := 0
	for _, m := range matches {
		if m.Similarity >= min {
			n++
		}
	}
	return n
}

// headline words level as an estimate and counts the closely-related
// matches. Every level but Open needs at least one, so with none the level is
// Open.
func headline(level model.Saturation, closeMatches int) string {
	belief := "We believe this space is " + strings.ToLower(level.String())
	if closeMatches == 0 {
		return belief + ": nothing close turned up in the sources checked. Keep looking before committing."
	}
	s := fmt.Sprintf("%s: %d closely-related tool%s turned up in the sources checked.", belief, closeMatches, plural(closeMatches))
	if level == model.Open {
		s += " Worth a look before committing."
	}
	return s
}

// plural returns "s" unless n is 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
