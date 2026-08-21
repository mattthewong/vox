package parakeet

import (
	"context"
	"errors"
	"testing"

	"vox/internal/transcribe"
)

func TestRecognizerSatisfiesTranscriber(t *testing.T) {
	var _ transcribe.Transcriber = (*Recognizer)(nil)
}

func TestTranscribeRejectsEmptyAudio(t *testing.T) {
	r := New(Config{ModelDir: t.TempDir()})
	if _, err := r.Transcribe(context.Background(), nil, transcribe.TranscribeOptions{}); err == nil {
		t.Error("expected error for empty audio")
	}
}

func TestTranscribeRejectsZeroSampleAudio(t *testing.T) {
	// A structurally valid WAV whose data chunk is empty. This must be
	// rejected before reaching sherpa: AcceptWaveform indexes samples[0]
	// directly and panics on an empty slice.
	//
	// Note this is a zero-SAMPLE guard, not a silence guard. Audio that is
	// long but silent still runs full inference and returns empty text,
	// which filterBlankStage then drops. That is deliberate: at the measured
	// RTF of 0.038 a silent 2s chunk costs ~76ms, which does not justify an
	// energy threshold and the tuning it would need.
	r := New(Config{ModelDir: t.TempDir()})
	wav := buildWAV(t, 16000, make([]int16, 0))
	if _, err := r.Transcribe(context.Background(), wav, transcribe.TranscribeOptions{}); err == nil {
		t.Error("expected error for zero-sample audio")
	}
}

func TestTranscribeRejectsMissingModel(t *testing.T) {
	r := New(Config{ModelDir: t.TempDir()}) // dir exists but has no ONNX files
	wav := buildWAV(t, 16000, []int16{1, 2, 3, 4})
	_, err := r.Transcribe(context.Background(), wav, transcribe.TranscribeOptions{})
	if err == nil {
		t.Fatal("expected error for missing model files")
	}
}

func TestNumThreadsDefaults(t *testing.T) {
	r := New(Config{ModelDir: "/tmp/x"})
	if r.numThreads != defaultNumThreads {
		t.Errorf("numThreads = %d, want %d", r.numThreads, defaultNumThreads)
	}
	r2 := New(Config{ModelDir: "/tmp/x", NumThreads: 4})
	if r2.numThreads != 4 {
		t.Errorf("numThreads = %d, want 4", r2.numThreads)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	r := New(Config{ModelDir: t.TempDir()})
	r.Close()
	r.Close() // must not panic
}

func TestTranscribeRespectsContextCancellation(t *testing.T) {
	r := New(Config{ModelDir: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	wav := buildWAV(t, 16000, []int16{1, 2, 3})
	_, err := r.Transcribe(ctx, wav, transcribe.TranscribeOptions{})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestTranscribeAfterCloseFails(t *testing.T) {
	r := New(Config{ModelDir: t.TempDir()})
	r.Close()

	wav := buildWAV(t, 16000, []int16{1, 2, 3})
	if _, err := r.Transcribe(context.Background(), wav, transcribe.TranscribeOptions{}); err == nil {
		t.Error("expected error transcribing on a closed recognizer")
	}
}
