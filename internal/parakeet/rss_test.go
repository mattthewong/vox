//go:build parakeet_integration

package parakeet

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"vox/internal/sttmodel"
	"vox/internal/transcribe"
)

// processRSSMB returns the current process RSS in megabytes by calling ps.
// runtime.MemStats only covers Go heap; the onnxruntime model weights live
// on the C heap and are invisible to Go. ps reports the real number.
func processRSSMB(t *testing.T) float64 {
	t.Helper()
	out, err := exec.Command("ps", "-o", "rss=", "-p",
		fmt.Sprintf("%d", os.Getpid())).Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	var kb int64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &kb); err != nil {
		t.Fatalf("parse ps output %q: %v", out, err)
	}
	return float64(kb) / 1024.0
}

// TestRSSStabilityAcrossTranscriptions verifies that repeated transcriptions
// on a single Recognizer (no destroy/recreate cycles) do not grow RSS.
//
// Run: go test -tags parakeet_integration -run TestRSSStability -v -count=1 -timeout=60s ./internal/parakeet/
func TestRSSStabilityAcrossTranscriptions(t *testing.T) {
	m, ok := sttmodel.ByID("parakeet-v2")
	if !ok {
		t.Fatal("parakeet-v2 not in catalog")
	}
	if !sttmodel.IsInstalled(m) {
		t.Skip("parakeet-v2 not installed; run: vox model download parakeet-v2")
	}
	dir, err := sttmodel.ResolvePath(m)
	if err != nil {
		t.Fatal(err)
	}
	wavPath := filepath.Join(dir, "test_wavs", "0.wav")
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		t.Skipf("test wav not found: %v", err)
	}

	runtime.GC()
	baseline := processRSSMB(t)
	t.Logf("Baseline: %.1f MB", baseline)

	rec := New(Config{ModelDir: dir})
	defer rec.Close()

	var rssValues []float64
	for i := 1; i <= 5; i++ {
		_, err := rec.Transcribe(context.Background(), wav, transcribe.TranscribeOptions{})
		if err != nil {
			t.Fatalf("Transcribe %d: %v", i, err)
		}
		runtime.GC()
		rss := processRSSMB(t)
		rssValues = append(rssValues, rss)
		t.Logf("After transcribe %d: %.1f MB  (+%.1f from baseline)", i, rss, rss-baseline)
	}

	// Verify RSS is stable (not growing) after the first transcription.
	// Allow 50 MB tolerance for GC jitter.
	peak := rssValues[0]
	last := rssValues[len(rssValues)-1]
	if last > peak+50 {
		t.Errorf("RSS grew from %.1f to %.1f MB across transcriptions (possible leak)", peak, last)
	} else {
		t.Logf("RSS stable: peak=%.1f MB, final=%.1f MB (no growth)", peak, last)
	}
}
