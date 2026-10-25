// Package textutil provides the character classes and case mapping of Rust's
// standard library, so that ported code tokenizes and compares text exactly as
// patent does. Go's unicode package draws some of these lines differently.
package textutil

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsAlphanumeric reports whether r is alphabetic or numeric in the sense of
// Rust's char::is_alphanumeric: the Unicode Alphabetic property (letters,
// letter numbers and Other_Alphabetic/Lowercase/Uppercase, which includes
// combining marks such as U+0345 and circled letters such as U+24D0) or a
// numeric category (Nd, Nl, No). unicode.IsLetter and unicode.IsNumber alone
// miss the Other_* properties.
func IsAlphanumeric(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) ||
		unicode.In(r, unicode.Other_Alphabetic, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// ToLower lowercases s like Rust's str::to_lowercase. It differs from
// strings.ToLower in the two places Rust applies full case mapping: U+0130
// (İ) becomes "i̇" (i plus a combining dot), not "i", and a capital sigma at
// the end of a word becomes the final form ς, not σ.
func ToLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		switch r {
		case 'Σ':
			if isFinalSigma(s, i) {
				b.WriteRune('ς')
			} else {
				b.WriteRune('σ')
			}
		case 'İ':
			b.WriteString("i̇")
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// isFinalSigma implements the Final_Sigma condition of the Unicode standard
// (section 3.13) the same way Rust's core library does: the sigma at byte
// offset i follows a cased letter and is not followed by one, skipping
// case-ignorable characters in both directions.
func isFinalSigma(s string, i int) bool {
	before := false
	for j := i; j > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
		if !isCaseIgnorable(r) {
			before = isCased(r)
			break
		}
	}
	if !before {
		return false
	}
	for _, r := range s[i+len("Σ"):] {
		if !isCaseIgnorable(r) {
			return !isCased(r)
		}
	}
	return true
}

func isCased(r rune) bool {
	return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// isCaseIgnorable approximates the Case_Ignorable derived property: the
// general categories Mn, Me, Cf, Lm and Sk plus the characters whose
// Word_Break value is MidLetter, MidNumLet or Single_Quote, which Go's
// unicode package has no table for.
func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', 0x00B7, 0x0387, 0x055F, 0x05F4, 0x2018, 0x2019, 0x2024, 0x2027,
		0xFE13, 0xFE52, 0xFE55, 0xFF07, 0xFF0E, 0xFF1A:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}
