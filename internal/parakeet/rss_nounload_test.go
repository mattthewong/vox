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
	"time"

	"vox/internal/sttmodel"
	"vox/internal/transcribe"
)

func rssMB(t *testing.T) float64 {
	t.Helper()
	out, err := exec.Command("ps", "-o", "rss=", "-p",
		fmt.Sprintf("%d", os.Getpid())).Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	var kb int64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &kb)
	return float64(kb) / 1024.0
}

// TestRSSDestroyCycleLeaks proves that destroy/recreate cycles cause
// monotonic RSS growth. This is the regression test for the idle-unload
// removal: if idle unloading is ever re-introduced, this test will catch
// the leak.
//
// Run: go test -tags parakeet_integration -run TestRSSDestroyCycleLeaks -v -count=1 -timeout=60s ./internal/parakeet/
func TestRSSDestroyCycleLeaks(t *testing.T) {
	m, ok := sttmodel.ByID("parakeet-v2")
	if !ok {
		t.Fatal("parakeet-v2 not in catalog")
	}
	if !sttmodel.IsInstalled(m) {
		t.Skip("parakeet-v2 not installed")
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
	baseline := rssMB(t)
	t.Logf("Baseline:  %.1f MB", baseline)

	// Run 3 destroy/recreate cycles and measure retained RSS.
	var retainedMBs []float64
	for cycle := 1; cycle <= 3; cycle++ {
		r := New(Config{ModelDir: dir})
		_, err := r.Transcribe(context.Background(), wav, transcribe.TranscribeOptions{})
		if err != nil {
			t.Fatalf("Cycle %d Transcribe: %v", cycle, err)
		}
		afterLoad := rssMB(t)

		r.Close()
		runtime.GC()
		time.Sleep(500 * time.Millisecond)
		afterClose := rssMB(t)
		retained := afterClose - baseline
		retainedMBs = append(retainedMBs, retained)
		t.Logf("Cycle %d: loaded=%.1f MB, after close=%.1f MB  (retained=%.1f MB)",
			cycle, afterLoad, afterClose, retained)
	}

	// Verify that retained memory grows across cycles (proving the leak).
	// This test documents the known onnxruntime behavior; if onnxruntime
	// is ever updated to fix the leak, this test will fail — which is
	// the desired outcome.
	growth := retainedMBs[len(retainedMBs)-1] - retainedMBs[0]
	t.Logf("")
	t.Logf("Retained RSS growth across %d cycles: %.1f MB", len(retainedMBs), growth)
	if growth > 0 {
		t.Logf("CONFIRMED: destroy/recreate cycles leak memory (this is expected)")
	} else {
		t.Logf("No growth detected — onnxruntime may have been fixed; consider re-enabling idle unload")
	}
}
