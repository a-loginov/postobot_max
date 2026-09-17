package moderation

import (
	"strings"
	"unicode"
)

// Moderation automatically checks a new request for profanity, spam and duplicates.
type Moderation struct {
	matWords     []string
	spamWords    []string
	simThreshold float64
}

func New(matWords, spamWords []string) *Moderation {
	return &Moderation{
		matWords:     matWords,
		spamWords:    spamWords,
		simThreshold: 0.8,
	}
}

type Result struct {
	Clean     bool
	HasMat    bool
	HasSpam   bool
	Duplicate bool
	Text      string
}

// Check normalizes text and runs both filters.
func (m *Moderation) Check(text string) Result {
	normalized := Normalize(text)
	lower := strings.ToLower(normalized)

	hasMat := m.hasMat(lower)
	hasSpam := m.hasSpam(lower)

	return Result{
		Clean:   !hasMat && !hasSpam,
		HasMat:  hasMat,
		HasSpam: hasSpam,
		Text:    normalized,
	}
}

// hasMat reports profanity using exact matches and evasion-resistant checks:
// the word is also compared against a letter-collapsed form of the text, so
// "бляяядь" / "бялдь"-style stretching is still caught.
func (m *Moderation) hasMat(lower string) bool {
	if lower == "" {
		return false
	}
	collapsed := collapse(lower)
	for _, w := range m.matWords {
		w = collapse(w)
		if strings.Contains(lower, w) || strings.Contains(collapsed, w) {
			return true
		}
	}
	return false
}

// hasSpam reports whether the text contains any spam words.
func (m *Moderation) hasSpam(lower string) bool {
	if lower == "" {
		return false
	}
	collapsed := collapse(lower)
	for _, w := range m.spamWords {
		w = strings.ToLower(w)
		wc := collapse(w)
		if strings.Contains(lower, w) || strings.Contains(collapsed, wc) {
			return true
		}
	}
	return false
}

// collapse removes adjacent duplicate runes ("бляяядь" -> "блядь").
func collapse(s string) string {
	var b strings.Builder
	var prev rune
	for i, r := range s {
		if i > 0 && r == prev {
			continue
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

// Normalize strips punctuation, collapses whitespace and lowercases a text.
func Normalize(text string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(text) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// IsSimilar reports whether candidate is a slight variation of existing
// (same text, a few words added/moved/typos — token overlap >= threshold).
// At least two shared words are required so short, terse comments
// ("тест", "стул") are never flagged as duplicates.
func (m *Moderation) IsSimilar(candidate, existing string) bool {
	common := sharedCount(tokens(candidate), tokens(existing))
	if common < 2 {
		return false
	}
	return Similarity(candidate, existing) >= m.simThreshold
}

// sharedCount returns how many words a and b have in common.
func sharedCount(a, b []string) int {
	setB := make(map[string]bool, len(b))
	for _, w := range b {
		setB[w] = true
	}
	count := 0
	for _, w := range a {
		if setB[w] {
			count++
		}
	}
	return count
}

// Similarity returns a Dice coefficient on word tokens, in [0, 1].
// Tokens shorter than 3 runes (particles etc.) are ignored.
func Similarity(a, b string) float64 {
	ta := tokens(a)
	tb := tokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}

	setB := make(map[string]bool, len(tb))
	for _, w := range tb {
		setB[w] = true
	}

	count := 0
	for _, w := range ta {
		if setB[w] {
			count++
		}
	}

	return 2 * float64(count) / float64(len(ta)+len(tb))
}

func tokens(text string) []string {
	var words []string
	set := make(map[string]bool)
	for _, w := range strings.Fields(text) {
		if len([]rune(w)) < 3 {
			continue
		}
		if !set[w] {
			set[w] = true
			words = append(words, w)
		}
	}
	return words
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
