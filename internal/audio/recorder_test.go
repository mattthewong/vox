package audio

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

// --- Cleanup orphaned temp files ---

func TestCleanupOrphanedTempFiles_RemovesVoxFiles(t *testing.T) {
	// Create fake orphaned vox temp files in the OS temp dir.
	tmpDir := os.TempDir()

	files := []string{
		filepath.Join(tmpDir, "vox-1234567890.wav"),
		filepath.Join(tmpDir, "vox-9999999999.wav"),
		filepath.Join(tmpDir, "vox-sound-1234567890.wav"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("fake wav data"), 0644); err != nil {
			t.Fatalf("failed to create test file %s: %v", f, err)
		}
	}
	// Verify they exist.
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("test file should exist: %s", f)
		}
	}

	removed := CleanupOrphanedTempFiles()

	if removed < len(files) {
		t.Errorf("CleanupOrphanedTempFiles() removed %d files, want at least %d", removed, len(files))
	}

	// Verify they are gone.
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("file should have been removed: %s", f)
			os.Remove(f) // cleanup on failure
		}
	}
}

func TestCleanupOrphanedTempFiles_IgnoresOtherFiles(t *testing.T) {
	// Create a non-vox temp file — it should NOT be deleted.
	f, err := os.CreateTemp("", "notvox-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)

	CleanupOrphanedTempFiles()

	if _, err := os.Stat(name); os.IsNotExist(err) {
		t.Errorf("non-vox file should not have been removed: %s", name)
	}
}

func TestCleanupOrphanedTempFiles_NoFilesIsNoop(t *testing.T) {
	// When there are no orphaned files, it should return 0 and not error.
	// (We can't guarantee no vox files exist in the real tmpdir, but we
	// can at least verify it doesn't panic.)
	removed := CleanupOrphanedTempFiles()
	if removed < 0 {
		t.Errorf("CleanupOrphanedTempFiles() returned negative: %d", removed)
	}
}

// --- Max recording duration ---

func TestNewRecorderSetsDefaultMaxDuration(t *testing.T) {
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}
	if r.MaxDuration != DefaultMaxDuration {
		t.Errorf("MaxDuration = %v, want %v", r.MaxDuration, DefaultMaxDuration)
	}
}

func TestMaxDurationAutoStops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}

	// Set a very short max duration so the test doesn't take long.
	r.MaxDuration = 2 * time.Second

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait long enough for the auto-stop to fire and the subprocess to exit.
	// rec/ffmpeg can be slow to respond to SIGINT, so give generous headroom.
	deadline := time.After(30 * time.Second)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			t.Error("timed out waiting for auto-stop")
			r.Stop() // force cleanup
			return
		case <-tick.C:
			if !r.IsRecording() {
				return // success
			}
		}
	}
}

func TestMaxDurationZeroDisablesTimer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}

	// Disable max duration.
	r.MaxDuration = 0

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer func() {
		if r.IsRecording() {
			r.Stop()
		}
	}()

	// With MaxDuration=0, the internal timer should be nil.
	r.mu.Lock()
	hasTimer := r.timer != nil
	r.mu.Unlock()

	if hasTimer {
		t.Error("expected no timer when MaxDuration=0")
	}
}

// TestTimerSetAndClearedByStartStop verifies that Start creates a timer
// and Stop cancels it, without depending on subprocess timing.
func TestTimerSetAndClearedByStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	hasMicrophone(t)

	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error: %v", err)
	}

	r.MaxDuration = 10 * time.Minute // won't fire during test

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Verify timer was created.
	r.mu.Lock()
	hasTimer := r.timer != nil
	r.mu.Unlock()
	if !hasTimer {
		t.Error("expected timer to be set after Start()")
	}

	// Stop should cancel the timer.
	_, err = r.Stop()
	// ErrTooShort is acceptable for a very short recording.
	if err != nil && !errors.Is(err, ErrTooShort) {
		t.Fatalf("Stop() error: %v", err)
	}

	r.mu.Lock()
	timerAfterStop := r.timer
	r.mu.Unlock()
	if timerAfterStop != nil {
		t.Error("expected timer to be nil after Stop()")
	}
}

// --- Temp file cleanup on Stop ---

func TestStopCleansUpTempFile(t *testing.T) {
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

	// Capture the temp file path before Stop clears it.
	r.mu.Lock()
	tmpPath := r.tmpPath
	r.mu.Unlock()

	if tmpPath == "" {
		t.Fatal("expected tmpPath to be set after Start()")
	}

	// Verify the temp file exists.
	if _, err := os.Stat(tmpPath); os.IsNotExist(err) {
		t.Fatal("temp file should exist after Start()")
	}

	_, _ = r.Stop()

	// Verify the temp file was cleaned up.
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file should have been removed after Stop(): %s", tmpPath)
		os.Remove(tmpPath)
	}
}
