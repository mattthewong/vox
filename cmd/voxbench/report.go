package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// clipResult is one transcribed clip.
type clipResult struct {
	Path     string
	WER      float64
	Decode   time.Duration
	AudioDur time.Duration
	Ref      string
	Hyp      string
	Err      error
}

// engineResult accumulates every clip for one engine plus its aggregates.
type engineResult struct {
	Engine    string
	LoadTime  time.Duration
	GoHeapSys uint64 // runtime.MemStats.Sys, NOT process RSS
	Clips     []clipResult

	MeanWER   float64
	P50Decode time.Duration
	P95Decode time.Duration
	RTF       float64
	Failures  int
}

// summarize computes aggregate statistics over the recorded clips. Clips that
// errored are counted as failures and excluded from WER and latency.
func (r *engineResult) summarize() {
	var (
		werSum     float64
		scored     int
		decodes    []time.Duration
		totalDec   time.Duration
		totalAudio time.Duration
	)
	for _, c := range r.Clips {
		if c.Err != nil {
			r.Failures++
			continue
		}
		werSum += c.WER
		scored++
		decodes = append(decodes, c.Decode)
		totalDec += c.Decode
		totalAudio += c.AudioDur
	}
	if scored == 0 {
		return
	}
	r.MeanWER = werSum / float64(scored)

	sort.Slice(decodes, func(i, j int) bool { return decodes[i] < decodes[j] })
	r.P50Decode = percentile(decodes, 0.50)
	r.P95Decode = percentile(decodes, 0.95)

	if totalAudio > 0 {
		r.RTF = totalDec.Seconds() / totalAudio.Seconds()
	}
}

// percentile returns the p-th percentile of a sorted slice using
// nearest-rank. Returns 0 for an empty slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted))*p+0.5) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// renderMarkdown formats one corpus's results as a comparison table.
func renderMarkdown(corpus string, results []*engineResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Corpus: %s\n\n", corpus)
	if len(results) > 0 {
		fmt.Fprintf(&b, "Clips: %d\n\n", len(results[0].Clips))
	}

	b.WriteString("| Engine | WER | p50 decode | p95 decode | RTF | Load | Go heap sys | Failures |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | %.2f%% | %s | %s | %.3f | %s | %s | %d |\n",
			r.Engine,
			r.MeanWER*100,
			r.P50Decode.Round(time.Millisecond),
			r.P95Decode.Round(time.Millisecond),
			r.RTF,
			r.LoadTime.Round(time.Millisecond),
			humanBytes(r.GoHeapSys),
			r.Failures,
		)
	}
	b.WriteString("\nLower WER and lower RTF are better. RTF below 1.0 means faster than real time.\n")
	b.WriteString("Go heap sys is `runtime.MemStats.Sys`; it excludes the ONNX runtime's\n")
	b.WriteString("native allocations, so it is a floor, not process RSS. Use\n")
	b.WriteString("`/usr/bin/time -l` when the real number matters.\n")
	return b.String()
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
