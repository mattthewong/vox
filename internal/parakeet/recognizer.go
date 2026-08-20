// Package parakeet runs NVIDIA Parakeet TDT speech recognition in-process via
// sherpa-onnx. Models are INT8 ONNX exports (encoder, decoder, joiner) that
// run on CPU with no GPU or Python dependency.
//
// The sherpa-onnx C library is dlopened on first use rather than linked, so
// vox processes that never select a Parakeet engine never map
// libonnxruntime.dylib. See sherpa_dyn.go for why that matters.
package parakeet

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vox/internal/transcribe"
)

const (
	// sampleRate and featureDim are fixed by the Parakeet model exports.
	sampleRate = 16000
	featureDim = 80

	// defaultIdleTimeout is how long an initialized recognizer is kept in
	// memory after its last use. The ONNX runtime holds roughly a gigabyte
	// resident, which is worth releasing between dictation sessions.
	defaultIdleTimeout = 5 * time.Minute

	defaultNumThreads = 2
)

// Config configures a Recognizer.
type Config struct {
	// ModelDir contains encoder.int8.onnx, decoder.int8.onnx,
	// joiner.int8.onnx, and tokens.txt.
	ModelDir string

	// NumThreads for ONNX inference. Defaults to 2.
	NumThreads int

	// IdleTimeout overrides how long the loaded model is retained.
	IdleTimeout time.Duration

	// Logger receives load and unload events. Optional; defaults to a
	// discard logger.
	Logger *slog.Logger
}

// Recognizer transcribes audio using a Parakeet model. It implements
// transcribe.Transcriber.
//
// The underlying sherpa-onnx recognizer is created lazily on first use and
// released after IdleTimeout. Transcribe is safe for concurrent use, though
// calls are serialized.
type Recognizer struct {
	modelDir    string
	numThreads  int
	idleTimeout time.Duration
	log         *slog.Logger

	mu        sync.Mutex
	impl      *recognizerHandle
	idleTimer *time.Timer
	closed    bool
}

// New creates a Recognizer. The model is not loaded until the first
// Transcribe call.
func New(cfg Config) *Recognizer {
	threads := cfg.NumThreads
	if threads <= 0 {
		threads = defaultNumThreads
	}
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Recognizer{
		modelDir:    cfg.ModelDir,
		numThreads:  threads,
		idleTimeout: idle,
		log:         log,
	}
}

// requiredFiles are the model artifacts sherpa-onnx needs.
var requiredFiles = []string{
	"encoder.int8.onnx",
	"decoder.int8.onnx",
	"joiner.int8.onnx",
	"tokens.txt",
}

// Transcribe decodes wavData and returns the recognized text.
// opts is accepted for interface compatibility; Parakeet has no equivalent of
// whisper's initial_prompt, and language is fixed by the chosen model.
func (r *Recognizer) Transcribe(ctx context.Context, wavData []byte, _ transcribe.TranscribeOptions) (string, error) {
	if len(wavData) == 0 {
		return "", fmt.Errorf("empty WAV data")
	}
	samples, rate, err := DecodeWAV(wavData)
	if err != nil {
		return "", fmt.Errorf("decode wav: %w", err)
	}
	if len(samples) == 0 {
		return "", fmt.Errorf("wav contains no samples")
	}
	if rate != sampleRate {
		return "", fmt.Errorf("unsupported sample rate %d (want %d)", rate, sampleRate)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return "", fmt.Errorf("recognizer is closed")
	}
	if err := r.ensureLoadedLocked(); err != nil {
		return "", err
	}

	text, err := r.impl.decode(samples, sampleRate)
	if err != nil {
		return "", err
	}

	r.resetIdleTimerLocked()
	return text, nil
}

// ensureLoadedLocked creates the sherpa recognizer if it is not resident.
// Caller must hold r.mu.
func (r *Recognizer) ensureLoadedLocked() error {
	if r.impl != nil {
		return nil
	}
	for _, f := range requiredFiles {
		p := filepath.Join(r.modelDir, f)
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			return fmt.Errorf("parakeet model file missing or empty: %s", p)
		}
	}

	start := time.Now()
	impl, err := newRecognizerHandle(r.modelDir, r.numThreads)
	if err != nil {
		return fmt.Errorf("create parakeet recognizer: %w", err)
	}
	r.impl = impl
	r.log.Debug("parakeet model loaded",
		"dir", r.modelDir,
		"load_ms", time.Since(start).Milliseconds())
	return nil
}

// resetIdleTimerLocked schedules model release. Caller must hold r.mu.
func (r *Recognizer) resetIdleTimerLocked() {
	if r.idleTimer != nil {
		r.idleTimer.Stop()
	}
	r.idleTimer = time.AfterFunc(r.idleTimeout, r.releaseIfIdle)
}

// releaseIfIdle frees the loaded model to return memory to the OS.
func (r *Recognizer) releaseIfIdle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unloadLocked()
}

// unloadLocked frees the sherpa recognizer. Caller must hold r.mu.
func (r *Recognizer) unloadLocked() {
	if r.impl != nil {
		r.impl.close()
		r.impl = nil
		// Worth logging: the next transcription pays a cold-start penalty of
		// roughly two seconds, and without this line that looks like an
		// unexplained latency spike when debugging.
		r.log.Debug("parakeet model released after idle timeout",
			"idle_timeout", r.idleTimeout)
	}
	if r.idleTimer != nil {
		r.idleTimer.Stop()
		r.idleTimer = nil
	}
}

// Close releases the model and prevents further use. It is safe to call
// multiple times.
func (r *Recognizer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unloadLocked()
	r.closed = true
}
