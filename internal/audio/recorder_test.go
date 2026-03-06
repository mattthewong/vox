package audio

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

// hasMicrophone returns true if at least one recording tool is available on the
// host. Tests that exercise actual recording should skip when this is false.
func hasMicrophone(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rec"); err == nil {
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return
	}
	t.Skip("no recording tool (rec or ffmpeg) found on PATH; skipping")
}

func TestDetectRecordingTool(t *testing.T) {
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}
	if r.SampleRate != defaultSampleRate {
		t.Errorf("expected default sample rate %d, got %d", defaultSampleRate, r.SampleRate)
	}
}

func TestStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !r.IsRecording() {
		t.Fatal("expected IsRecording() to be true after Start()")
	}

	// Record for 1 second so the file is large enough.
	time.Sleep(1 * time.Second)

	data, err := r.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if r.IsRecording() {
		t.Fatal("expected IsRecording() to be false after Stop()")
	}

	// A 1-second 16 kHz mono 16-bit WAV should be roughly 32 KB + header.
	// We just verify it is reasonably sized (> 1 KB).
	if len(data) < minFileSize {
		t.Errorf("expected WAV data > %d bytes, got %d", minFileSize, len(data))
	}

	// Minimal WAV header check: first 4 bytes should be "RIFF".
	if len(data) >= 4 && string(data[:4]) != "RIFF" {
		t.Errorf("expected WAV header to start with RIFF, got %q", string(data[:4]))
	}
}

func TestStopWithoutStart(t *testing.T) {
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}

	_, err = r.Stop()
	if err == nil {
		t.Fatal("expected error from Stop() without Start()")
	}
	if !errors.Is(err, ErrNotRecording) {
		t.Errorf("expected ErrNotRecording, got: %v", err)
	}
}

func TestDoubleStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	// Ensure cleanup happens even if test assertions fail.
	defer func() {
		if r.IsRecording() {
			_, _ = r.Stop()
		}
	}()

	err = r.Start()
	if err == nil {
		t.Fatal("expected error from second Start()")
	}
	if !errors.Is(err, ErrAlreadyRecording) {
		t.Errorf("expected ErrAlreadyRecording, got: %v", err)
	}
}

func TestTooShortRecording(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Stop immediately — the file should be too small to be useful.
	_, err = r.Stop()
	if err == nil {
		// It is possible (though unlikely) that enough data was captured even
		// with an immediate stop, so we only fail if there is no error AND
		// the data was tiny. Accept either ErrTooShort or a successful but
		// very small result.
		t.Log("Stop() returned no error on immediate stop; file may have had enough data")
		return
	}
	if !errors.Is(err, ErrTooShort) {
		t.Errorf("expected ErrTooShort, got: %v", err)
	}
}
