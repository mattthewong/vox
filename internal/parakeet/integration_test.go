//go:build parakeet_integration

package parakeet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vox/internal/sttmodel"
	"vox/internal/transcribe"
)

// TestTranscribeRealModel runs the installed parakeet-v2 model against the
// test WAV bundled in its own release archive.
//
// Run with: make test-parakeet
func TestTranscribeRealModel(t *testing.T) {
	m, ok := sttmodel.ByID("parakeet-v2")
	if !ok {
		t.Fatal("parakeet-v2 not in catalog")
	}
	if !sttmodel.IsInstalled(m) {
		t.Skip("parakeet-v2 not installed; run `vox model download parakeet-v2` first")
	}
	dir, err := sttmodel.ResolvePath(m)
	if err != nil {
		t.Fatal(err)
	}

	wavPath := filepath.Join(dir, "test_wavs", "0.wav")
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		t.Skipf("bundled test wav not found at %s: %v", wavPath, err)
	}

	r := New(Config{ModelDir: dir})
	defer r.Close()

	got, err := r.Transcribe(context.Background(), wav, transcribe.TranscribeOptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("transcript is empty")
	}
	t.Logf("transcript: %q", got)

	// Parakeet emits punctuation and capitalization natively; assert the
	// output looks like real prose rather than a bare token stream.
	if got != strings.ToLower(got) {
		t.Log("output contains capitalization, as expected")
	} else {
		t.Error("expected capitalized output from parakeet")
	}
}
