package pipeline

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/model"
)

const example = "e.g.\n  silt \"CLI tool that kills a process on a given port\""

// The idea errors are shown to users verbatim, as patent words them.
//
//nolint:staticcheck // ST1005: capitalized and punctuated on purpose.
var (
	errNoIdea     = errors.New("Please provide a dev-tool idea to search for.")
	errVague      = errors.New("Too vague — describe a specific software tool or feature, " + example)
	errRepetitive = errors.New("Too repetitive — describe what the tool does, " + example)
)

// stopwords are left out of the keywords sent to registries, and don't count
// towards an idea being specific enough.
var stopwords = func() map[string]bool {
	m := make(map[string]bool)
	for _, w := range strings.Fields(`
		the and for are but not you all can had her was one our out has its let may
		who did get got how his him she also been call each from have into just like
		long make many more most much must name only over some such than that them
		then they this very when what with will your which about after being could
		every first found great where these their there those would other should
		before between best near here well does were`) {
		m[w] = true
	}
	return m
}()

func countRunes(s string, f func(rune) bool) int {
	n := 0
	for _, r := range s {
		if f(r) {
			n++
		}
	}
	return n
}

// ValidateIdea rejects ideas too empty, vague or repetitive to search for.
//
// A word is a whitespace-delimited token with at least three alphanumeric
// characters, in any script. An idea needs three distinct words, two of them
// not stopwords. Scripts that don't separate words with spaces (Chinese,
// Japanese) can't be tokenized that way, so an idea of fewer than three
// tokens is accepted if it has at least two non-ASCII alphanumeric
// characters. Gating on non-ASCII keeps that from becoming an escape hatch for
// short ASCII gibberish.
func ValidateIdea(idea string) error {
	trimmed := strings.TrimSpace(idea)
	if trimmed == "" {
		return errNoIdea
	}
	tokens := strings.Fields(trimmed)
	var meaningful []string
	for _, w := range tokens {
		if countRunes(w, textutil.IsAlphanumeric) >= 3 {
			meaningful = append(meaningful, textutil.ToLower(w))
		}
	}
	cjkLike := countRunes(trimmed, func(r rune) bool { return textutil.IsAlphanumeric(r) && r >= utf8.RuneSelf })
	if len(tokens) < 3 && cjkLike >= 2 {
		return nil
	}
	if len(meaningful) < 3 {
		return errVague
	}
	unique := make(map[string]bool)
	for _, w := range meaningful {
		unique[w] = true
	}
	if len(unique) < 3 {
		return errRepetitive
	}
	nonStopwords := 0
	for _, w := range meaningful {
		if !stopwords[w] {
			nonStopwords++
		}
	}
	if nonStopwords < 2 {
		return errVague
	}
	return nil
}

// BuildQuery derives the registry keywords from idea: its words lowercased,
// stripped of punctuation other than '-', and kept if longer than two bytes
// and not a stopword.
func BuildQuery(idea string) model.Query {
	var keywords []string
	for _, w := range strings.Fields(idea) {
		w = strings.Map(func(r rune) rune {
			if textutil.IsAlphanumeric(r) || r == '-' {
				return r
			}
			return -1
		}, textutil.ToLower(w))
		if len(w) > 2 && !stopwords[w] {
			keywords = append(keywords, w)
		}
	}
	return model.Query{Idea: idea, Keywords: keywords}
}
