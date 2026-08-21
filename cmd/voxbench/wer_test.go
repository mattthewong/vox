package main

import "testing"

func TestNormalizeStripsPunctuationAndCase(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Hello, world!", "hello world"},
		{"  Multiple   spaces  ", "multiple spaces"},
		{"It's fine.", "it's fine"},
		{"Dr. Smith said: \"go\"", "dr smith said go"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalize(tt.in); got != tt.want {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWERIdentical(t *testing.T) {
	if got := wer("the quick brown fox", "the quick brown fox"); got != 0 {
		t.Errorf("wer = %v, want 0", got)
	}
}

func TestWERPunctuationOnlyDifferenceIsZero(t *testing.T) {
	// The whole point of normalizing: parakeet's native punctuation must not
	// be scored as errors against an unpunctuated reference.
	if got := wer("the quick brown fox", "The quick, brown fox."); got != 0 {
		t.Errorf("wer = %v, want 0", got)
	}
}

func TestWERSubstitution(t *testing.T) {
	// One substitution out of four reference words.
	got := wer("the quick brown fox", "the quick brown dog")
	if got != 0.25 {
		t.Errorf("wer = %v, want 0.25", got)
	}
}

func TestWERDeletionAndInsertion(t *testing.T) {
	if got := wer("a b c d", "a b c"); got != 0.25 {
		t.Errorf("deletion wer = %v, want 0.25", got)
	}
	if got := wer("a b c d", "a b c d e"); got != 0.25 {
		t.Errorf("insertion wer = %v, want 0.25", got)
	}
}

func TestWEREmptyReference(t *testing.T) {
	if got := wer("", ""); got != 0 {
		t.Errorf("wer = %v, want 0", got)
	}
	if got := wer("", "spurious words"); got != 1 {
		t.Errorf("wer = %v, want 1 for empty reference with output", got)
	}
}

func TestWERCompleteMiss(t *testing.T) {
	if got := wer("a b", "x y"); got != 1 {
		t.Errorf("wer = %v, want 1", got)
	}
}
