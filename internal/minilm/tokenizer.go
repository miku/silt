package minilm

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxTokens is the truncation length: fastembed truncates to
// min(512, model_max_length), and the model's position embeddings stop at
// 512 too. It includes [CLS] and [SEP].
const maxTokens = 512

// Tokenizer is the BERT WordPiece tokenizer of tokenizer.json, configured as
// fastembed configures the Hugging Face tokenizers library: BertNormalizer
// (clean text, isolate CJK ideographs, strip accents, lowercase),
// BertPreTokenizer, WordPiece, [CLS] … [SEP], truncated to 512 tokens.
type Tokenizer struct {
	vocab   map[string]int32
	special []specialToken // the added tokens, matched before normalizing
	unk     int32
	cls     int32
	sep     int32
	// maxWordChars: longer words become [UNK] without being split.
	maxWordChars int
}

type specialToken struct {
	content string
	id      int32
}

// NewTokenizer parses a tokenizer.json. It accepts only the configuration
// it implements, so a different model fails loudly rather than tokenizing
// subtly wrong.
func NewTokenizer(tokenizerJSON []byte) (*Tokenizer, error) {
	var tj struct {
		AddedTokens []struct {
			ID         int32  `json:"id"`
			Content    string `json:"content"`
			SingleWord bool   `json:"single_word"`
			Lstrip     bool   `json:"lstrip"`
			Rstrip     bool   `json:"rstrip"`
			Normalized bool   `json:"normalized"`
			Special    bool   `json:"special"`
		} `json:"added_tokens"`
		Normalizer struct {
			Type               string `json:"type"`
			CleanText          bool   `json:"clean_text"`
			HandleChineseChars bool   `json:"handle_chinese_chars"`
			StripAccents       *bool  `json:"strip_accents"`
			Lowercase          bool   `json:"lowercase"`
		} `json:"normalizer"`
		PreTokenizer struct {
			Type string `json:"type"`
		} `json:"pre_tokenizer"`
		Model struct {
			Type                    string           `json:"type"`
			UnkToken                string           `json:"unk_token"`
			ContinuingSubwordPrefix string           `json:"continuing_subword_prefix"`
			MaxInputCharsPerWord    int              `json:"max_input_chars_per_word"`
			Vocab                   map[string]int32 `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(tokenizerJSON, &tj); err != nil {
		return nil, fmt.Errorf("tokenizer.json: %w", err)
	}
	n := tj.Normalizer
	stripAccents := n.StripAccents == nil || *n.StripAccents
	if n.Type != "BertNormalizer" || !n.CleanText || !n.HandleChineseChars || !stripAccents || !n.Lowercase {
		return nil, fmt.Errorf("tokenizer.json: unsupported normalizer %+v", n)
	}
	if tj.PreTokenizer.Type != "BertPreTokenizer" {
		return nil, fmt.Errorf("tokenizer.json: unsupported pre-tokenizer %q", tj.PreTokenizer.Type)
	}
	m := tj.Model
	if m.Type != "WordPiece" || m.ContinuingSubwordPrefix != "##" {
		return nil, fmt.Errorf("tokenizer.json: unsupported model %q (prefix %q)", m.Type, m.ContinuingSubwordPrefix)
	}
	t := &Tokenizer{vocab: m.Vocab, maxWordChars: m.MaxInputCharsPerWord}
	for name, dst := range map[string]*int32{m.UnkToken: &t.unk, "[CLS]": &t.cls, "[SEP]": &t.sep} {
		id, ok := m.Vocab[name]
		if !ok {
			return nil, fmt.Errorf("tokenizer.json: no %s in the vocabulary", name)
		}
		*dst = id
	}
	for _, a := range tj.AddedTokens {
		if a.SingleWord || a.Lstrip || a.Rstrip || a.Normalized {
			return nil, fmt.Errorf("tokenizer.json: unsupported added token %+v", a)
		}
		t.special = append(t.special, specialToken{a.Content, a.ID})
	}
	// Leftmost-longest matching: try longer tokens first at each position.
	slices.SortStableFunc(t.special, func(a, b specialToken) int { return len(b.content) - len(a.content) })
	return t, nil
}

// Encode returns the token ids of text: [CLS], at most 510 content tokens,
// [SEP].
func (t *Tokenizer) Encode(text string) []int32 {
	ids := []int32{t.cls}
	for text != "" {
		pos, tok := t.nextSpecial(text)
		ids = t.appendText(ids, text[:pos])
		if tok == nil {
			break
		}
		ids = append(ids, tok.id)
		text = text[pos+len(tok.content):]
	}
	if len(ids) > maxTokens-1 {
		ids = ids[:maxTokens-1]
	}
	return append(ids, t.sep)
}

// nextSpecial finds the leftmost added token in s (the longest, if several
// start there). Added tokens are matched in the raw text, before
// normalizing, so "[MASK]" in an idea is the mask token.
func (t *Tokenizer) nextSpecial(s string) (int, *specialToken) {
	best, bestPos := -1, len(s)
	for i := range t.special {
		if p := strings.Index(s, t.special[i].content); p >= 0 && p < bestPos {
			best, bestPos = i, p
		}
	}
	if best < 0 {
		return len(s), nil
	}
	return bestPos, &t.special[best]
}

func (t *Tokenizer) appendText(ids []int32, s string) []int32 {
	for _, word := range preTokenize(normalize(s)) {
		ids = t.appendWordPiece(ids, word)
		if len(ids) >= maxTokens {
			break // the rest would be truncated anyway
		}
	}
	return ids
}

// appendWordPiece splits word greedily into the longest vocabulary entries,
// continuations prefixed with "##". A word that can't be split, or is longer
// than maxWordChars, is a single [UNK].
func (t *Tokenizer) appendWordPiece(ids []int32, word string) []int32 {
	if utf8.RuneCountInString(word) > t.maxWordChars {
		return append(ids, t.unk)
	}
	start := len(ids)
	for begin := 0; begin < len(word); {
		end := len(word)
		var id int32 = -1
		for begin < end {
			sub := word[begin:end]
			if begin > 0 {
				sub = "##" + sub
			}
			if v, ok := t.vocab[sub]; ok {
				id = v
				break
			}
			_, size := utf8.DecodeLastRuneInString(word[begin:end])
			end -= size
		}
		if id < 0 {
			return append(ids[:start], t.unk)
		}
		ids = append(ids, id)
		begin = end
	}
	return ids
}

// isWhitespace is Rust's char::is_whitespace, the White_Space property; Go's
// unicode.IsSpace is the same set.
func isWhitespace(r rune) bool { return unicode.IsSpace(r) }

// isControl is the clean_text notion of a control character: categories Cc,
// Cf and Co, except tab, newline and carriage return.
func isControl(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return false
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co)
}

func isChineseChar(r rune) bool {
	return 0x4E00 <= r && r <= 0x9FFF ||
		0x3400 <= r && r <= 0x4DBF ||
		0x20000 <= r && r <= 0x2A6DF ||
		0x2A700 <= r && r <= 0x2B73F ||
		0x2B740 <= r && r <= 0x2B81F ||
		0x2B920 <= r && r <= 0x2CEAF ||
		0xF900 <= r && r <= 0xFAFF ||
		0x2F800 <= r && r <= 0x2FA1F
}

// isPunctuation is BertPreTokenizer's: ASCII punctuation (which includes
// symbols such as $ + < = > ^ ` | ~) or a Unicode punctuation category.
func isPunctuation(r rune) bool {
	if r < utf8.RuneSelf {
		return '!' <= r && r <= '/' || ':' <= r && r <= '@' || '[' <= r && r <= '`' || '{' <= r && r <= '~'
	}
	return unicode.IsPunct(r)
}

// normalize is BertNormalizer: drop NUL, U+FFFD and control characters and
// map whitespace to spaces; surround CJK ideographs with spaces; decompose
// (NFD) and drop nonspacing marks; lowercase each character. Lowercasing
// comes after the decomposition, so U+0130 (whose full lowercase is two
// characters) has already become a plain I by then.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == 0 || r == utf8.RuneError || isControl(r):
		case isWhitespace(r):
			b.WriteByte(' ')
		case isChineseChar(r):
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	decomposed := norm.NFD.String(b.String())
	b.Reset()
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'İ': // the one character whose lowercase is two
			b.WriteString("i̇")
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// preTokenize is BertPreTokenizer: split on whitespace, and make every
// punctuation character a word of its own.
func preTokenize(s string) []string {
	var words []string
	for _, field := range strings.FieldsFunc(s, isWhitespace) {
		start := 0
		for i, r := range field {
			if isPunctuation(r) {
				if start < i {
					words = append(words, field[start:i])
				}
				words = append(words, field[i:i+utf8.RuneLen(r)])
				start = i + utf8.RuneLen(r)
			}
		}
		if start < len(field) {
			words = append(words, field[start:])
		}
	}
	return words
}
