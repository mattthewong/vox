//go:build smoke_manual

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vox/internal/sttmodel"
	"vox/internal/transcribe"
)

// TestSmokeParakeetEndToEnd drives the real engine factory with the real
// installed model and asserts the two claims that matter:
//  1. a parakeet engine constructs no whisperserver
//  2. the resulting transcriber actually produces text
//
// Run: go test -tags smoke_manual ./cmd/vox/ -run SmokeParakeet -v
func TestSmokeParakeetEndToEnd(t *testing.T) {
	m, err := resolveEngineModel("parakeet-v2")
	if err != nil {
		t.Fatalf("resolveEngineModel: %v", err)
	}
	if m.ID != "parakeet-v2" {
		t.Fatalf("resolved %q, want parakeet-v2", m.ID)
	}
	if !sttmodel.IsInstalled(m) {
		t.Skip("parakeet-v2 not installed")
	}

	eng, degraded, err := startEngineWithFallback(context.Background(), m, os.DevNull, nil)
	if err != nil {
		t.Fatalf("startEngineWithFallback: %v", err)
	}
	defer eng.stop(context.Background())

	if degraded {
		t.Error("degraded should be false")
	}
	if eng.whisperSrv != nil {
		t.Fatal("CLAIM VIOLATED: parakeet engine constructed a whisperserver")
	}
	if eng.whisperClient != nil {
		t.Error("parakeet engine should have no whisper HTTP client")
	}
	t.Log("confirmed: no whisperserver for parakeet engine")

	// Drive a real transcription through the holder, the way the pipeline does.
	holder := &engineHolder{}
	holder.set(eng)

	dir, _ := sttmodel.ResolvePath(m)
	wav, err := os.ReadFile(filepath.Join(dir, "test_wavs", "0.wav"))
	if err != nil {
		t.Skipf("bundled test wav unavailable: %v", err)
	}

	got, err := holder.Transcribe(context.Background(), wav, transcribe.TranscribeOptions{})
	if err != nil {
		t.Fatalf("Transcribe through holder: %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("empty transcript")
	}
	t.Logf("transcript: %q", got)

	// Parakeet's selling point: punctuation and capitalization with no AI pass.
	if got == strings.ToLower(got) {
		t.Error("expected capitalization in parakeet output")
	}
	if !strings.ContainsAny(got, ".,?!") {
		t.Error("expected punctuation in parakeet output")
	}
}

// TestSmokeWhisperStillStarts confirms the whisper path is unbroken by the
// refactor.
//
// This test MUST skip when port 2022 is already bound. whisperserver.Start
// spawns a child that fails to bind, then waitReady's health probe gets a 200
// from the process already on the port, so Start returns nil and the test
// passes green while proving nothing. Detect the collision explicitly rather
// than trusting a fast pass.
func TestSmokeWhisperStillStarts(t *testing.T) {
	if c, err := net.DialTimeout("tcp", "127.0.0.1:2022", 300*time.Millisecond); err == nil {
		c.Close()
		t.Skip("port 2022 already bound (vox running?) — a result here would be a false green")
	}

	m, err := resolveEngineModel("base.en")
	if err != nil {
		t.Fatal(err)
	}
	if !sttmodel.IsInstalled(m) {
		t.Skip("base.en not installed")
	}
	eng, _, err := startEngineWithFallback(context.Background(), m, os.DevNull, nil)
	if err != nil {
		t.Fatalf("whisper start failed: %v", err)
	}
	defer eng.stop(context.Background())

	if eng.whisperSrv == nil {
		t.Fatal("CLAIM VIOLATED: whisper engine did not construct a whisperserver")
	}
	if eng.parakeetRec != nil {
		t.Error("whisper engine should have no parakeet recognizer")
	}
	t.Log("confirmed: whisperserver constructed for whisper engine")
}
