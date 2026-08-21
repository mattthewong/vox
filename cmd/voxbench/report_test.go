package main

import (
	"strings"
	"testing"
	"time"
)

func TestSummarizeComputesAggregates(t *testing.T) {
	r := &engineResult{
		Engine:   "parakeet-v2",
		LoadTime: 2 * time.Second,
		Clips: []clipResult{
			{WER: 0.0, Decode: 100 * time.Millisecond, AudioDur: time.Second},
			{WER: 0.5, Decode: 300 * time.Millisecond, AudioDur: time.Second},
			{WER: 0.25, Decode: 200 * time.Millisecond, AudioDur: time.Second},
			{WER: 0.25, Decode: 400 * time.Millisecond, AudioDur: time.Second},
		},
	}
	r.summarize()

	if r.MeanWER != 0.25 {
		t.Errorf("MeanWER = %v, want 0.25", r.MeanWER)
	}
	if r.P50Decode != 200*time.Millisecond && r.P50Decode != 300*time.Millisecond {
		t.Errorf("P50Decode = %v, want 200ms or 300ms", r.P50Decode)
	}
	if r.P95Decode != 400*time.Millisecond {
		t.Errorf("P95Decode = %v, want 400ms", r.P95Decode)
	}
	// Total decode 1.0s over total audio 4.0s.
	if r.RTF != 0.25 {
		t.Errorf("RTF = %v, want 0.25", r.RTF)
	}
}

func TestSummarizeEmptyClips(t *testing.T) {
	r := &engineResult{Engine: "empty"}
	r.summarize() // must not panic or divide by zero
	if r.MeanWER != 0 {
		t.Errorf("MeanWER = %v, want 0", r.MeanWER)
	}
}

func TestRenderMarkdownIncludesAllEngines(t *testing.T) {
	results := []*engineResult{
		{Engine: "base.en", MeanWER: 0.12, P50Decode: 300 * time.Millisecond, P95Decode: 500 * time.Millisecond, RTF: 0.3, Clips: make([]clipResult, 10)},
		{Engine: "parakeet-v2", MeanWER: 0.06, P50Decode: 150 * time.Millisecond, P95Decode: 250 * time.Millisecond, RTF: 0.15, Clips: make([]clipResult, 10)},
	}
	out := renderMarkdown("libri-subset", results)

	for _, want := range []string{"libri-subset", "base.en", "parakeet-v2", "WER", "RTF", "p95"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "12.00%") {
		t.Errorf("report should render WER as a percentage\n%s", out)
	}
}
