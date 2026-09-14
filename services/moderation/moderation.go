package moderation

import (
	"strings"
	"unicode"
)

// Moderation automatically checks a new request for profanity and duplicates.
type Moderation struct {
	matWords     []string
	simThreshold float64
}

func New(matWords []string) *Moderation {
	return &Moderation{
		matWords:     matWords,
		simThreshold: 0.8,
	}
}

type Result struct {
	Clean     bool
	HasMat    bool
	Duplicate bool
	Text      string
}

// Check normalizes text and runs both filters.
func (m *Moderation) Check(text string) Result {
	normalized := Normalize(text)

	hasMat := false
	lower := strings.ToLower(normalized)
	for _, w := range m.matWords {
		if strings.Contains(lower, w) {
			hasMat = true
			break
		}
	}

	return Result{
		Clean:  !hasMat,
		HasMat: hasMat,
		Text:   normalized,
	}
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
func (m *Moderation) IsSimilar(candidate, existing string) bool {
	return Similarity(candidate, existing) >= m.simThreshold
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
