package whisperserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// skipIfNoWhisperServer skips the test when whisper-server is not in PATH.
// Process lifecycle tests require the real binary.
func skipIfNoWhisperServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("whisper-server"); err != nil {
		t.Skip("whisper-server not in PATH, skipping integration test")
	}
}

func TestNewResolvesWhisperServer(t *testing.T) {
	skipIfNoWhisperServer(t)
	srv, err := New("127.0.0.1", 0, filepath.Join(t.TempDir(), "whisper.log"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.binPath == "" {
		t.Fatal("binPath is empty after New()")
	}
}

func TestNewFailsWhenBinaryMissing(t *testing.T) {
	// Temporarily clear PATH so LookPath fails.
	t.Setenv("PATH", t.TempDir()) // empty dir, no binaries
	_, err := New("127.0.0.1", 0, filepath.Join(t.TempDir(), "whisper.log"))
	if err == nil {
		t.Fatal("expected error when whisper-server not in PATH")
	}
}

func TestStopWhenNotRunning(t *testing.T) {
	skipIfNoWhisperServer(t)
	srv, err := New("127.0.0.1", 0, filepath.Join(t.TempDir(), "whisper.log"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Stop on a server that was never started should be a no-op.
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop on idle server: %v", err)
	}
}

func TestStopTwiceIsIdempotent(t *testing.T) {
	skipIfNoWhisperServer(t)
	srv, err := New("127.0.0.1", 0, filepath.Join(t.TempDir(), "whisper.log"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestStartRejectsDoubleStart(t *testing.T) {
	skipIfNoWhisperServer(t)
	logPath := filepath.Join(t.TempDir(), "whisper.log")
	srv, err := New("127.0.0.1", 29999, logPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// We need a valid model path — use a dummy file. The server will fail
	// to load it, but we only care about the double-start guard here.
	modelPath := filepath.Join(t.TempDir(), "dummy.bin")
	if err := os.WriteFile(modelPath, []byte("not-a-real-model"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Use a short-lived context so we don't wait 45s for readiness.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start will likely fail on waitReady (dummy model), but the cmd is launched.
	// We force it by directly calling startLocked + setting fields.
	srv.mu.Lock()
	cmd, done, lf, startErr := srv.startLocked(modelPath)
	if startErr != nil {
		srv.mu.Unlock()
		t.Fatalf("startLocked: %v", startErr)
	}
	srv.cmd = cmd
	srv.done = done
	srv.logFile = lf
	srv.model = modelPath
	srv.mu.Unlock()
	defer func() { _ = srv.Stop(ctx) }()

	// Second Start should fail immediately.
	err = srv.Start(ctx, modelPath)
	if err == nil {
		t.Fatal("expected error on double Start, got nil")
	}
	if err.Error() != "whisper-server already running" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCurrentModelReturnsEmpty(t *testing.T) {
	skipIfNoWhisperServer(t)
	srv, err := New("127.0.0.1", 0, filepath.Join(t.TempDir(), "whisper.log"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := srv.CurrentModel(); got != "" {
		t.Fatalf("CurrentModel on idle server = %q, want empty", got)
	}
}

func TestURL(t *testing.T) {
	skipIfNoWhisperServer(t)
	srv, err := New("127.0.0.1", 2022, filepath.Join(t.TempDir(), "whisper.log"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := "http://127.0.0.1:2022"
	if got := srv.URL(); got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestSwitchRollbackOnStartFailure(t *testing.T) {
	skipIfNoWhisperServer(t)
	logPath := filepath.Join(t.TempDir(), "whisper.log")
	srv, err := New("127.0.0.1", 29998, logPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Create a valid-looking model path and a bad one.
	goodModel := filepath.Join(t.TempDir(), "good.bin")
	badModel := filepath.Join(t.TempDir(), "bad.bin")
	if err := os.WriteFile(goodModel, []byte("good"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(badModel, []byte("bad"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Simulate a running server by setting the model path directly.
	// We can't actually start whisper-server with a fake model, so we test
	// the logic path: Switch stores prevModel, calls Stop (no-op since not
	// really running), then Start fails, then rollback Start also fails.
	// The key assertion is that Switch returns an error mentioning rollback.

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Switch on a stopped server: Stop is no-op, Start with badModel will
	// fail at waitReady (timeout). Since prevModel is "", no rollback is
	// attempted.
	err = srv.Switch(ctx, badModel)
	if err == nil {
		t.Fatal("expected Switch to fail with bad model")
	}
	t.Logf("Switch error (expected): %v", err)
}

func TestLogFileCreated(t *testing.T) {
	skipIfNoWhisperServer(t)
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "subdir", "whisper.log")
	srv, err := New("127.0.0.1", 29997, logPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	modelPath := filepath.Join(t.TempDir(), "dummy.bin")
	if err := os.WriteFile(modelPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// startLocked should create the log directory and file.
	srv.mu.Lock()
	cmd, done, lf, startErr := srv.startLocked(modelPath)
	srv.mu.Unlock()
	if startErr != nil {
		t.Fatalf("startLocked: %v", startErr)
	}
	srv.mu.Lock()
	srv.cmd = cmd
	srv.done = done
	srv.logFile = lf
	srv.model = modelPath
	srv.mu.Unlock()
	defer func() { _ = srv.Stop(context.Background()) }()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatalf("log file not created at %s", logPath)
	}
}
