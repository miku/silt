// Package verdict turns ranked matches into an integrity-scoped verdict.
//
// [BuildPrompt] asks an LLM backend for a verdict, and the prompt forbids
// claiming non-existence: results are always phrased as "found in the sources
// checked", and a clean result means "keep looking before committing", never
// a green light. [FromData] derives a verdict from the similarity data alone.
package verdict

import (
	"fmt"
	"strings"
	"time"

	"github.com/miku/silt/freshness"
	"github.com/miku/silt/model"
)

// Caveat is the fixed, humble caveat shown on every verdict. Never weaken
// this.
const Caveat = "Not proof it doesn't exist — only that nothing close turned up " +
	"in the sources checked. Keep looking (web, app stores, niche communities) before committing."

// sourceList renders the sources actually searched, for the prompt.
func sourceList(checked []model.Source) string {
	if len(checked) == 0 {
		return "the selected open-source registries"
	}
	names := make([]string, len(checked))
	for i, s := range checked {
		names[i] = s.String()
	}
	return strings.Join(names, ", ")
}

// BuildPrompt builds the LLM prompt that enforces the integrity rules.
//
// checked must be the sources that actually responded: the prompt only ever
// tells the model about coverage that really happened, so the model can't be
// steered into claiming a source was searched when it wasn't.
func BuildPrompt(q model.Query, matches []model.Match, checked []model.Source) string {
	return buildPrompt(q, matches, checked, freshness.Now())
}

// buildPrompt is BuildPrompt with the current time as a parameter, so that
// the ages it renders are reproducible in tests.
func buildPrompt(q model.Query, matches []model.Match, checked []model.Source, now time.Time) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are a skeptical prior-art analyst for SOFTWARE DEVELOPER TOOLS ONLY. Your "+
		"default assumption is that the idea has already been built — lean toward Crowded "+
		"or Saturated when in doubt. The user has an idea for a dev tool and we searched "+
		"these open-source sources for existing implementations: %s.\n\n", sourceList(checked))

	fmt.Fprintf(&b, "## Idea\n%s\n\n", q.Idea)

	if len(matches) == 0 {
		b.WriteString("## Matches\nNo matches were found in the sources checked.\n\n")
	} else {
		top10 := matches[:min(10, len(matches))]
		var sum float32
		for _, m := range top10 {
			sum += m.Similarity
		}
		avg := sum / float32(len(top10))

		b.WriteString("## Matches found (ranked by cosine similarity to the idea)\n")
		fmt.Fprintf(&b, "Top-10 average similarity: %.2f (scale: 0.0 = unrelated, 0.5 = tangential, "+
			"0.7+ = strong match). Each match also shows a popularity figure (a per-source "+
			"signal of how established it is — stars, downloads, etc.) and its URL; a strong "+
			"match that is also popular is firmer prior art. Where the source reports one, a "+
			"match also shows when it was last updated. Treat that as colour on the prior art, "+
			"never as a reason to discount it: a tool that exists but looks unmaintained is "+
			"still proof the idea has been built, and may be worth mentioning as a space with "+
			"room for a maintained alternative. Matches with no date shown are ones whose "+
			"source does not publish one — that is not evidence of staleness.\n\n", avg)
		for _, m := range matches[:min(15, len(matches))] {
			var pop, updated, url string
			if m.Popularity != nil {
				pop = fmt.Sprintf(", popularity %d", *m.Popularity)
			}
			if strings.TrimSpace(m.URL) != "" {
				url = " — " + m.URL
			}
			if m.LastUpdated != nil {
				if age, ok := freshness.AgeOf(*m.LastUpdated, now); ok {
					updated = ", updated " + age.Label
				}
			}
			fmt.Fprintf(&b, "- **%s** (%s, sim %.2f%s%s)%s: %s\n",
				m.Name, m.Source, m.Similarity, pop, updated, url, m.Description)
		}
		if len(matches) > 15 {
			fmt.Fprintf(&b, "- … and %d more with lower similarity\n", len(matches)-15)
		}
		b.WriteByte('\n')
	}

	b.WriteString("## Rules — you MUST follow these\n" +
		"- You can prove something EXISTS; you must NEVER claim something does not exist.\n" +
		"- All conclusions must be scoped to \"found in the sources checked\".\n" +
		"- Do not say \"this doesn't exist\" or \"there is no prior art\" — only that " +
		"nothing close turned up in the sources checked.\n" +
		"- If the idea is NOT about software, developer tools, or programming, respond " +
		"with level \"Open\" and headline \"This does not appear to be a software tool " +
		"idea — silt searches developer tool registries only.\"\n" +
		"- Focus ONLY on matches that directly address the SPECIFIC feature described in " +
		"the idea. Generic or tangential tools (e.g. a generic linter when the idea is " +
		"a specific kind of linter) do NOT count as prior art.\n\n")

	b.WriteString("## How to choose the level\n" +
		"Use the similarity scores — they measure how closely each match relates to the idea:\n" +
		"- **Open**: no match has similarity >= 0.55, OR matches are only tangentially " +
		"related (they share a category but not the specific feature).\n" +
		"- **Crowded**: at least 2-3 matches with similarity >= 0.55 that directly " +
		"address the same problem.\n" +
		"- **Saturated**: 5+ strong matches (>= 0.60) covering the idea with little room " +
		"for differentiation.\n\n")

	b.WriteString("## Output\n" +
		"Respond with ONLY a JSON object (no markdown fences, no commentary):\n" +
		"```\n" +
		"{\n  " +
		"\"level\": \"Open\" | \"Crowded\" | \"Saturated\",\n  " +
		"\"headline\": \"one-sentence summary scoped to sources checked\",\n  " +
		"\"gaps\": [\"gap the user could fill\", ...]\n" +
		"}\n" +
		"```\n" +
		"The headline MUST describe the user's idea and its closest matches above " +
		"— never an unrelated tool from the list — and must be scoped to the " +
		"sources checked. Never claim the idea does not exist or has no prior art.\n")

	return b.String()
}

// countAtLeast counts the matches with similarity of at least min.
func countAtLeast(matches []model.Match, min float32) int {
	n := 0
	for _, m := range matches {
		if m.Similarity >= min {
			n++
		}
	}
	return n
}

// plural returns "s" unless n is 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// dataHeadline is a safe, scoped headline derived purely from the data. It
// never asserts absence.
func dataHeadline(level model.Saturation, matches []model.Match) string {
	closeMatches := countAtLeast(matches, 0.55)
	switch level {
	case model.Saturated:
		return fmt.Sprintf("Saturated — %d closely-related tools turned up in the sources checked.", closeMatches)
	case model.Crowded:
		return fmt.Sprintf("Crowded — %d closely-related tool%s turned up in the sources checked.", closeMatches, plural(closeMatches))
	default:
		if closeMatches == 0 {
			return "Nothing close turned up in the sources checked — keep looking before committing."
		}
		return fmt.Sprintf("%d closely-related tool%s turned up, but the space still looks open in "+
			"the sources checked. Worth a look before committing.", closeMatches, plural(closeMatches))
	}
}

// floorLevel floors the model's level against the similarity data, so that
// it can never hand out a green-light Open when the embeddings clearly show
// close prior art.
func floorLevel(modelLevel model.Saturation, matches []model.Match) model.Saturation {
	strong := countAtLeast(matches, 0.60)
	closeMatches := countAtLeast(matches, 0.55)
	// A single near-identical match (>= 0.70) already means the space isn't
	// open.
	veryStrong := countAtLeast(matches, 0.70)
	dataLevel := model.Open
	switch {
	case strong >= 5:
		dataLevel = model.Saturated
	case closeMatches >= 2 || veryStrong >= 1:
		dataLevel = model.Crowded
	}
	return max(modelLevel, dataLevel)
}

// FromData builds a verdict from the similarity data alone, without calling
// a model.
//
// This is the --fast path (and that of any caller that deliberately skips the
// LLM). The level is derived purely by flooring against the similarity data,
// and the headline is the same safe, scoped, data-only sentence the flooring
// guard produces — so a run without an LLM still gives an honest signal,
// never flashes a misleading green Open over a clearly populated space, and
// still carries the fixed integrity [Caveat]. Gaps require a model, so they
// are empty here.
func FromData(matches []model.Match, checked, failed []model.Source) model.Verdict {
	level := floorLevel(model.Open, matches)
	return model.Verdict{
		Level:          level,
		Headline:       dataHeadline(level, matches),
		Gaps:           []string{},
		SourcesChecked: checked,
		SourcesFailed:  failed,
		Caveat:         Caveat,
	}
}
