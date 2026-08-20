package main

import (
	"strings"
	"unicode"
)

// normalize prepares a transcript for scoring: lowercase, punctuation removed,
// whitespace collapsed. Apostrophes are kept so "it's" and "its" stay
// distinct. This is what makes the comparison fair between whisper (which
// emits little punctuation) and parakeet (which punctuates natively).
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '\'':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			// Drop punctuation entirely rather than splitting words on it, so
			// "co-op" becomes "coop" consistently on both sides.
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// wer returns the word error rate of hypothesis against reference, computed as
// Levenshtein edit distance over word tokens divided by reference length.
// Both inputs are normalized first. The result is not capped at 1.0 except
// when the reference is empty.
func wer(reference, hypothesis string) float64 {
	ref := strings.Fields(normalize(reference))
	hyp := strings.Fields(normalize(hypothesis))

	if len(ref) == 0 {
		if len(hyp) == 0 {
			return 0
		}
		return 1
	}

	// Two-row dynamic programming table over word tokens.
	prev := make([]int, len(hyp)+1)
	curr := make([]int, len(hyp)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ref); i++ {
		curr[0] = i
		for j := 1; j <= len(hyp); j++ {
			cost := 1
			if ref[i-1] == hyp[j-1] {
				cost = 0
			}
			curr[j] = min3(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution or match
			)
		}
		prev, curr = curr, prev
	}
	return float64(prev[len(hyp)]) / float64(len(ref))
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
