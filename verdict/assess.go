package verdict

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miku/silt"
	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/llm"
	"github.com/miku/silt/model"
)

// absencePhrases assert non-existence. The integrity rule forbids ever
// telling a user their idea doesn't exist (we only searched some sources), so
// if the model emits one of these despite the prompt, we replace the text.
//
// This is a deliberately broad, conservative backstop: a false positive only
// downgrades the copy to a safe scoped headline, whereas a false negative is
// an integrity violation — so we err toward catching more.
var absencePhrases = []string{
	"does not exist",
	"doesn't exist",
	"do not exist",
	"don't exist",
	"no prior art",
	"nothing exists",
	"nothing like this",
	"never been built",
	"never been made",
	"never been implemented",
	"has not been built",
	"hasn't been built",
	"has not been implemented",
	"hasn't been implemented",
	"not been implemented",
	"no one has built",
	"no one has made",
	"no one else",
	"nobody else",
	"no one is doing",
	"no such tool",
	"no existing tool",
	"no existing solution",
	"no existing implementation",
	"no similar tool",
	"no similar project",
	"no comparable",
	"no competitors",
	"no alternatives",
	"no equivalent",
	"there is no tool",
	"there are no tools",
	"there is no existing",
	"there is no software",
	"there is no prior",
	"completely novel",
	"entirely new",
	"brand new concept",
	"first of its kind",
	"unprecedented",
	"greenfield",
	"wide open",
	"untapped",
	"gap in the market",
	"we found zero",
	"zero implementations",
	"zero results",
	"no one is solving",
	// Maintenance-flavoured absence. Once matches carry a last-updated date
	// the model can assert absence in a second way — not "nothing exists" but
	// "nothing *maintained* exists" — and that is the same unprovable claim:
	// only 9 of the 19 sources publish a date at all, so an undated match is
	// not an unmaintained one. These are the phrasings that generalise beyond
	// what was searched; saying a *specific* match is unmaintained ("the
	// closest match is no longer maintained") stays legal, because that is
	// read off the data we actually showed.
	"no maintained",
	"no actively developed",
	"nothing maintained",
	"no up-to-date",
}

// hedgeWords are adverbs a model slips between a negation and its noun ("no
// *actively* maintained tool"), which would otherwise walk an absence claim
// straight past a literal substring match. They are stripped from the text
// *and* from the phrases before comparing, so absencePhrases can stay written
// in natural English.
var hedgeWords = []string{
	"actively",
	"currently",
	"still",
	"truly",
	"genuinely",
	"really",
	"properly",
	"widely",
}

// stripHedges lowercases text and drops the hedges above, so "no currently
// maintained tool" collapses to the "no maintained tool" the phrase list is
// written against.
func stripHedges(text string) string {
	out := textutil.ToLower(text)
	for _, hedge := range hedgeWords {
		out = strings.ReplaceAll(out, hedge+" ", "")
	}
	return out
}

// containsAbsencePhrase reports whether text asserts that something does not
// exist.
func containsAbsencePhrase(text string) bool {
	lower := textutil.ToLower(text)
	hedgeless := stripHedges(lower)
	for _, p := range absencePhrases {
		if strings.Contains(lower, p) || strings.Contains(hedgeless, stripHedges(p)) {
			return true
		}
	}
	return false
}

// noMatchPhrases claim that nothing was found. That is fine when the matches
// are weak, but misleading when a genuinely close match is present, so they
// are guarded against in parseVerdict.
var noMatchPhrases = []string{
	"no direct match",
	"no close match",
	"no matching",
	"no matches found",
	"no match found",
	"no relevant match",
	"no clear match",
	"no exact match",
	"nothing closely related",
	"no direct prior art",
	"couldn't find any",
	"could not find any",
}

// claimsNoMatch reports whether text claims that nothing was found.
func claimsNoMatch(text string) bool {
	lower := textutil.ToLower(text)
	for _, p := range noMatchPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// guardHeadline replaces any headline that asserts non-existence with a safe
// scoped one. This is the code-level guarantee behind the integrity rule; the
// prompt asks the model to comply, but we never *rely* on it.
func guardHeadline(headline string, level model.Saturation, matches []model.Match) string {
	if containsAbsencePhrase(headline) {
		return dataHeadline(level, matches)
	}
	return headline
}

// isWholeWord reports whether word appears in text as a whole word, not as
// part of another word. Word characters are Unicode alphanumerics, unlike the
// ASCII ones of the source selection.
//
// As in patent, the character before a later occurrence is looked up in what
// is left after the previous one, so in "atat" the second "at" counts as a
// whole word.
func isWholeWord(text, word string) bool {
	if word == "" {
		return false
	}
	remaining := text
	for {
		pos := strings.Index(remaining, word)
		if pos < 0 {
			return false
		}
		before, n := utf8.DecodeLastRuneInString(remaining[:pos])
		after, m := utf8.DecodeRuneInString(remaining[pos+len(word):])
		if (n == 0 || !textutil.IsAlphanumeric(before)) && (m == 0 || !textutil.IsAlphanumeric(after)) {
			return true
		}
		remaining = remaining[pos+len(word):]
	}
}

// extractJSON extracts the JSON from a model response that may be wrapped in
// markdown fences.
func extractJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	// Reasoning models in the DeepSeek-R1 family emit their trace inline as a
	// <think>...</think> preamble instead of in a separate field. The trace
	// often drafts a fenced JSON block of its own, so it has to be dropped
	// before the fence scan below -- otherwise we parse the draft rather than
	// the answer. Slice past the last closing tag rather than matching a
	// pair: some chat templates open the block for the model, so the
	// response can carry </think> with no opener. An unterminated trace is
	// left alone and fails to parse, which is the honest outcome.
	if end := strings.LastIndex(trimmed, "</think>"); end >= 0 {
		trimmed = strings.TrimSpace(trimmed[end+len("</think>"):])
	}
	if start := strings.Index(trimmed, "```"); start >= 0 {
		content := strings.TrimLeftFunc(strings.TrimPrefix(trimmed[start+3:], "json"), unicode.IsSpace)
		if end := strings.Index(content, "```"); end >= 0 {
			return strings.TrimSpace(content[:end])
		}
	}
	return trimmed
}

var levels = map[string]model.Saturation{
	"Open":      model.Open,
	"Crowded":   model.Crowded,
	"Saturated": model.Saturated,
}

// parseVerdict parses the model's JSON response into the verdict fields we
// need, then applies the integrity guards: floor the level against the
// similarity data, and replace any headline that asserts non-existence.
func parseVerdict(raw string, matches []model.Match, checked, failed []model.Source) (model.Verdict, error) {
	var v any
	if err := json.Unmarshal([]byte(extractJSON(raw)), &v); err != nil {
		return model.Verdict{}, &silt.ParseError{Msg: err.Error()}
	}
	obj, _ := v.(map[string]any)

	s, isString := obj["level"].(string)
	modelLevel, ok := levels[s]
	if !isString || !ok {
		// Worded like patent's {:?} of an Option<&str>.
		other := "None"
		if isString {
			other = fmt.Sprintf("Some(%q)", s)
		}
		return model.Verdict{}, &silt.ParseError{Msg: "invalid level: " + other}
	}

	rawHeadline, ok := obj["headline"].(string)
	if !ok {
		return model.Verdict{}, &silt.ParseError{Msg: "missing 'headline'"}
	}

	// Gaps render verbatim, so they get the same absence-claim guard as the
	// headline: a gap that asserts non-existence is dropped rather than
	// shown. Also drop any gap that names a top-10 match — if the model
	// mentions a known tool in a gap it is confirming it exists, not
	// identifying open space.
	var topNames []string
	for _, m := range matches[:min(10, len(matches))] {
		topNames = append(topNames, textutil.ToLower(m.Name))
	}
	gaps := []string{}
	arr, _ := obj["gaps"].([]any)
	for _, g := range arr {
		gap, ok := g.(string)
		if !ok || containsAbsencePhrase(gap) || namesAny(textutil.ToLower(gap), topNames) {
			continue
		}
		gaps = append(gaps, gap)
	}

	// Floor the level against the data. If that raises it, the model
	// misjudged the space, so we don't trust its headline either — derive a
	// safe one.
	level := floorLevel(modelLevel, matches)
	headline := rawHeadline
	if level != modelLevel {
		headline = dataHeadline(level, matches)
	}
	headline = guardHeadline(headline, level, matches)

	// A close match (>= 0.55) is real prior art, so a "found nothing"
	// headline would be misleading even when the level stays Open. Replace it
	// with the data-derived headline, which names the close matches.
	closeMatches := countAtLeast(matches, 0.55)
	if closeMatches >= 1 && claimsNoMatch(headline) {
		headline = dataHeadline(level, matches)
	}

	// When the level is Open and nothing close turned up, always use the data
	// headline. The LLM may emit confident-sounding copy that reads as a
	// green light even when the matches are too weak to trust. The data
	// headline is the honest signal here.
	if level == model.Open && closeMatches == 0 {
		headline = dataHeadline(level, matches)
	}

	return model.Verdict{
		Level:          level,
		Headline:       headline,
		Gaps:           gaps,
		SourcesChecked: checked,
		SourcesFailed:  failed,
		Caveat:         Caveat,
	}, nil
}

// namesAny reports whether text contains any of names as a whole word.
func namesAny(text string, names []string) bool {
	for _, name := range names {
		if isWholeWord(text, name) {
			return true
		}
	}
	return false
}

// retryDelay is the pause before the one retry of a failed generation. Tests
// shorten it.
var retryDelay = 800 * time.Millisecond

// Assess produces a verdict on the ranked matches through an LLM backend.
//
// checked must be the sources that actually responded, as for [BuildPrompt];
// failed are passed through to the verdict. An HTTP error or an unreachable
// backend is retried once.
func Assess(ctx context.Context, backend llm.LLM, q model.Query, matches []model.Match, checked, failed []model.Source) (model.Verdict, error) {
	prompt := BuildPrompt(q, matches, checked)
	raw, err := backend.Generate(ctx, prompt)
	if err != nil {
		var (
			httpErr     *silt.HTTPError
			unreachable *silt.LLMUnreachableError
		)
		if !errors.As(err, &httpErr) && !errors.As(err, &unreachable) {
			return model.Verdict{}, err
		}
		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return model.Verdict{}, ctx.Err()
		}
		if raw, err = backend.Generate(ctx, prompt); err != nil {
			return model.Verdict{}, err
		}
	}
	return parseVerdict(raw, matches, checked, failed)
}
