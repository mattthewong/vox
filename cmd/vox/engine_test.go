package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vox/internal/sttmodel"
	"vox/internal/transcribe"
)

func TestResolveEngineModelExplicitModel(t *testing.T) {
	got, err := resolveEngineModel("parakeet-v2")
	if err != nil {
		t.Fatalf("resolveEngineModel: %v", err)
	}
	if got.ID != "parakeet-v2" {
		t.Errorf("ID = %q, want parakeet-v2", got.ID)
	}
	if got.Engine != sttmodel.EngineParakeet {
		t.Errorf("Engine = %q, want parakeet", got.Engine)
	}
}

func TestResolveEngineModelBareEngineName(t *testing.T) {
	// "whisper" is an engine, not a model ID. It must resolve to that
	// engine's default model rather than erroring.
	got, err := resolveEngineModel("whisper")
	if err != nil {
		t.Fatalf("resolveEngineModel: %v", err)
	}
	if got.ID != sttmodel.DefaultID {
		t.Errorf("ID = %q, want %q", got.ID, sttmodel.DefaultID)
	}
}

func TestResolveEngineModelBareParakeet(t *testing.T) {
	got, err := resolveEngineModel("parakeet")
	if err != nil {
		t.Fatalf("resolveEngineModel: %v", err)
	}
	if got.ID != "parakeet-v2" {
		t.Errorf("ID = %q, want parakeet-v2 (parakeet default)", got.ID)
	}
}

func TestResolveEngineModelUnknownFallsBackToDefault(t *testing.T) {
	got, err := resolveEngineModel("bogus-engine")
	if err != nil {
		t.Fatalf("resolveEngineModel: %v", err)
	}
	if got.ID != sttmodel.DefaultID {
		t.Errorf("ID = %q, want %q", got.ID, sttmodel.DefaultID)
	}
}

func TestResolveEngineModelEmptyUsesDefault(t *testing.T) {
	got, err := resolveEngineModel("")
	if err != nil {
		t.Fatalf("resolveEngineModel: %v", err)
	}
	if got.ID != sttmodel.DefaultID {
		t.Errorf("ID = %q, want %q", got.ID, sttmodel.DefaultID)
	}
}

// blockingTranscriber signals when a call is in flight and blocks until
// released, so a test can attempt an engine swap mid-transcription.
type blockingTranscriber struct {
	entered chan struct{}
	release chan struct{}
	stopped *atomic.Bool
}

func (b *blockingTranscriber) Transcribe(context.Context, []byte, transcribe.TranscribeOptions) (string, error) {
	close(b.entered)
	<-b.release
	if b.stopped.Load() {
		return "", fmt.Errorf("backend was stopped mid-transcription")
	}
	return "ok", nil
}

func TestHolderSwapWaitsForInFlightTranscription(t *testing.T) {
	var stopped atomic.Bool
	bt := &blockingTranscriber{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		stopped: &stopped,
	}

	h := &engineHolder{}
	h.set(&engine{
		model:       sttmodel.Model{ID: "fake-a"},
		transcriber: bt,
		onStop:      func() { stopped.Store(true) },
	})

	result := make(chan error, 1)
	go func() {
		_, err := h.Transcribe(context.Background(), []byte("wav"), transcribe.TranscribeOptions{})
		result <- err
	}()
	<-bt.entered // transcription is now in flight

	swapDone := make(chan struct{})
	go func() {
		_ = h.swap(context.Background(), &engine{
			model:       sttmodel.Model{ID: "fake-b"},
			transcriber: &fakeTranscriber{text: "b"},
		})
		close(swapDone)
	}()

	// The swap must block while the read lock is held.
	select {
	case <-swapDone:
		t.Fatal("swap completed while a transcription was in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(bt.release)

	if err := <-result; err != nil {
		t.Errorf("in-flight transcription failed: %v", err)
	}
	select {
	case <-swapDone:
	case <-time.After(2 * time.Second):
		t.Fatal("swap did not complete after the transcription finished")
	}
	if h.get().model.ID != "fake-b" {
		t.Errorf("active model = %q, want fake-b", h.get().model.ID)
	}
}

func TestFallbackModelIsWhisperDefault(t *testing.T) {
	got, ok := fallbackModel()
	if !ok {
		t.Fatal("fallbackModel should always resolve")
	}
	if got.ID != sttmodel.DefaultID {
		t.Errorf("ID = %q, want %q", got.ID, sttmodel.DefaultID)
	}
	if got.Engine != sttmodel.EngineWhisper {
		t.Errorf("Engine = %q, want whisper", got.Engine)
	}
}

func TestStartEngineWithFallbackReportsDegradation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	// Hermeticity, two parts. Without them this test downloads 142 MiB of
	// base.en and then spawns whisper-server against port 2022:
	//   1. Install a placeholder base.en so the fallback's IsInstalled check
	//      short-circuits its download.
	//   2. Empty PATH so whisperserver.New fails at exec.LookPath, before any
	//      child process is started or any port is bound. On a machine where
	//      whisper-server is installed and vox is already running, the health
	//      probe would otherwise answer from the *existing* server and report
	//      a fallback that never really started.
	installModel(t, "base.en")
	t.Setenv("PATH", t.TempDir())

	// A parakeet model whose files are absent and whose URL is unreachable,
	// so the download attempt fails fast rather than pulling 460 MiB.
	broken := sttmodel.Model{
		ID:       "parakeet-broken",
		Engine:   sttmodel.EngineParakeet,
		Dirname:  "parakeet-broken",
		URL:      "https://127.0.0.1:1/nope.tar.bz2",
		Checksum: "00",
		Files:    []string{"encoder.int8.onnx"},
	}

	eng, degraded, err := startEngineWithFallback(context.Background(), broken, os.DevNull, nil)
	if eng != nil {
		t.Cleanup(func() { _ = eng.stop(context.Background()) })
	}
	if err == nil {
		t.Fatal("expected an error describing the primary failure")
	}
	if !degraded {
		t.Error("degraded should be true when the primary engine fails")
	}
	// The primary failure must survive into the returned error; that is the
	// only thing the caller can show the user to explain the degradation.
	if !strings.Contains(err.Error(), "parakeet-broken") {
		t.Errorf("error %q does not name the model that failed", err)
	}
}

func TestStartEngineWithFallbackNoDegradationOnSuccess(t *testing.T) {
	// Guard the contract without starting a real backend. Parakeet is the
	// right engine for that: startEngine only constructs a Recognizer, which
	// loads ONNX lazily on first Transcribe, so nothing is downloaded, no
	// subprocess is spawned, and no port is bound.
	isolateModelDirs(t)
	m := installArchiveModel(t, "parakeet-v2")

	eng, degraded, err := startEngineWithFallback(context.Background(), m, os.DevNull, nil)
	if err != nil {
		t.Fatalf("startEngineWithFallback: %v", err)
	}
	defer eng.stop(context.Background())

	if degraded {
		t.Error("degraded should be false when the primary engine starts")
	}
	if eng.model.ID != m.ID {
		t.Errorf("model = %q, want %q", eng.model.ID, m.ID)
	}
	// The headline requirement of the parakeet work: selecting a parakeet
	// model must not bring up the whisper child process.
	if eng.whisperSrv != nil {
		t.Error("a parakeet engine must not start whisper-server")
	}
}
