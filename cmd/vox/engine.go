package main

import (
	"context"
	"fmt"
	"sync"

	"vox/internal/parakeet"
	"vox/internal/sttmodel"
	"vox/internal/transcribe"
	"vox/internal/whisperserver"
)

// parakeetDefaultID is the model used when the user selects the parakeet
// engine without naming a specific version.
const parakeetDefaultID = "parakeet-v2"

// resolveEngineModel maps a configured value to a concrete catalog model.
// The value may be a model ID ("parakeet-v2", "base.en") or a bare engine
// name ("whisper", "parakeet"). Anything unrecognized falls back to the
// default model rather than failing startup.
func resolveEngineModel(value string) (sttmodel.Model, error) {
	if m, ok := sttmodel.ByID(value); ok {
		return m, nil
	}
	switch sttmodel.Engine(value) {
	case sttmodel.EngineParakeet:
		if m, ok := sttmodel.ByID(parakeetDefaultID); ok {
			return m, nil
		}
	case sttmodel.EngineWhisper:
		// fall through to default
	}
	m, ok := sttmodel.ByID(sttmodel.DefaultID)
	if !ok {
		return sttmodel.Model{}, fmt.Errorf("default model %q missing from catalog", sttmodel.DefaultID)
	}
	return m, nil
}

// engine holds the active transcriber and any process it owns.
// Exactly one of whisperSrv or parakeetRec is non-nil.
type engine struct {
	model       sttmodel.Model
	transcriber transcribe.Transcriber

	whisperSrv    *whisperserver.Server
	whisperClient *transcribe.Client
	parakeetRec   *parakeet.Recognizer

	// onStop, when set, runs at the end of stop. Tests use it to observe
	// teardown; production paths leave it nil.
	onStop func()
}

// startEngine downloads the model if needed and brings up the backend for it.
// For whisper this spawns the whisper-server child process; for parakeet it
// constructs an in-process recognizer and starts no subprocess.
func startEngine(ctx context.Context, m sttmodel.Model, logPath string, onProgress func(int64, int64)) (*engine, error) {
	if !sttmodel.IsInstalled(m) {
		if err := sttmodel.Download(ctx, m, onProgress); err != nil {
			return nil, fmt.Errorf("download %s: %w", m.ID, err)
		}
	}
	path, err := sttmodel.ResolvePath(m)
	if err != nil {
		return nil, err
	}

	switch m.Engine {
	case sttmodel.EngineParakeet:
		rec := parakeet.New(parakeet.Config{ModelDir: path})
		return &engine{
			model:       m,
			transcriber: rec,
			parakeetRec: rec,
		}, nil

	case sttmodel.EngineWhisper:
		srv, err := whisperserver.New("127.0.0.1", 2022, logPath)
		if err != nil {
			return nil, err
		}
		if err := srv.Start(ctx, path); err != nil {
			return nil, fmt.Errorf("start whisper-server: %w", err)
		}
		client := transcribe.NewClient(srv.URL())
		return &engine{
			model:         m,
			transcriber:   client,
			whisperSrv:    srv,
			whisperClient: client,
		}, nil

	default:
		return nil, fmt.Errorf("unknown engine %q for model %s", m.Engine, m.ID)
	}
}

// fallbackModel is the engine vox drops back to when the configured one
// cannot start. Whisper is the safe choice: it is the default, it is the most
// likely model to already be on disk, and it needs no ONNX runtime.
func fallbackModel() (sttmodel.Model, bool) {
	return sttmodel.ByID(sttmodel.DefaultID)
}

// startEngineWithFallback tries the requested model and, if that fails, falls
// back to the default whisper model so vox stays usable. The returned bool
// reports whether a fallback happened, and the returned error carries the
// original failure so the caller can explain it to the user.
//
// A non-nil engine with a non-nil error means "running, but degraded".
//
// Known limitation: this covers startup and engine switches only. If the ONNX
// runtime fails partway through a session (OOM, a corrupted model page), the
// error surfaces on that transcription and the engine stays selected; there is
// no automatic demotion to whisper. That is deliberate for v1. Distinguishing
// a transient decode failure from a permanently broken engine needs failure
// accounting we have no data to calibrate yet, and demoting on a single error
// would silently change the user's engine mid-session.
func startEngineWithFallback(
	ctx context.Context,
	m sttmodel.Model,
	logPath string,
	onProgress func(int64, int64),
) (*engine, bool, error) {
	eng, err := startEngine(ctx, m, logPath, onProgress)
	if err == nil {
		return eng, false, nil
	}
	primaryErr := err

	fb, ok := fallbackModel()
	if !ok || fb.ID == m.ID {
		return nil, true, primaryErr
	}
	fbEng, fbErr := startEngine(ctx, fb, logPath, onProgress)
	if fbErr != nil {
		return nil, true, fmt.Errorf("%w (fallback to %s also failed: %v)", primaryErr, fb.ID, fbErr)
	}
	return fbEng, true, primaryErr
}

// stop shuts down whatever backend this engine owns.
func (e *engine) stop(ctx context.Context) error {
	if e == nil {
		return nil
	}
	defer func() {
		if e.onStop != nil {
			e.onStop()
		}
	}()
	if e.parakeetRec != nil {
		e.parakeetRec.Close()
		e.parakeetRec = nil
	}
	if e.whisperSrv != nil {
		srv := e.whisperSrv
		e.whisperSrv = nil
		return srv.Stop(ctx)
	}
	return nil
}

// healthCheck verifies the backend is reachable. Parakeet is in-process, so
// it has nothing to check.
func (e *engine) healthCheck(ctx context.Context) error {
	if e.whisperClient != nil {
		return e.whisperClient.HealthCheck(ctx)
	}
	return nil
}

// engineHolder lets the pipeline read the current transcriber without being
// rebuilt when the user switches engines from the menubar.
//
// The RWMutex is load-bearing, not just a race guard. Transcribe holds the
// read lock for the whole call, and swap holds the write lock across both the
// pointer update and the old engine's shutdown. Without that, a menubar
// switch could SIGTERM whisper-server (or close a Parakeet recognizer) while
// a transcription is still in flight: recording has already stopped by then,
// so activateModel's IsRecording guard does not cover this window.
//
// The cost is that an engine switch waits for any in-flight transcription,
// bounded by the transcribe client's own timeout. That is the correct
// tradeoff; dropping the user's last utterance to make a menu click feel
// snappy is not.
type engineHolder struct {
	mu     sync.RWMutex
	active *engine
}

// set installs an engine without stopping anything. Used once at startup.
func (h *engineHolder) set(e *engine) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.active = e
}

// get returns the active engine. Callers must not retain the pointer across a
// possible swap; use swap or Transcribe instead.
func (h *engineHolder) get() *engine {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.active
}

// swap installs next and stops the engine it replaces, holding the write lock
// across both so no transcription can be running against the old backend.
func (h *engineHolder) swap(ctx context.Context, next *engine) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.active
	h.active = next
	if prev == nil || prev == next {
		return nil
	}
	return prev.stop(ctx)
}

// stopActive shuts down the active engine and clears it. Used at shutdown.
func (h *engineHolder) stopActive(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		return nil
	}
	err := h.active.stop(ctx)
	h.active = nil
	return err
}

// withWriteLock runs fn with the holder's write lock held, so callers can
// mutate the active engine in place without racing a transcription.
func (h *engineHolder) withWriteLock(fn func() error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return fn()
}

// Transcribe implements transcribe.Transcriber by delegating to the engine
// that is active at call time, holding the read lock so the backend cannot be
// torn down mid-call.
func (h *engineHolder) Transcribe(ctx context.Context, wav []byte, opts transcribe.TranscribeOptions) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.active == nil || h.active.transcriber == nil {
		return "", fmt.Errorf("no speech engine is active")
	}
	return h.active.transcriber.Transcribe(ctx, wav, opts)
}
