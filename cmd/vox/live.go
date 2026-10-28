package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"vox/internal/audio"
	"vox/internal/overlay"
	"vox/internal/transcribe"
)

// livePreviewInterval is how often the in-progress recording is re-transcribed.
// Each pass transcribes everything captured so far, so a slow pass simply
// delays the next tick rather than queueing work. Parakeet v3 decodes ~30s of
// audio in under a second on Apple silicon, so the tick, not the model, sets
// how far the overlay trails speech.
const livePreviewInterval = 400 * time.Millisecond

// preview is nil unless the vox-streaming-overlay flag is on. Set once in run
// before any goroutine that reads it starts.
var preview *livePreview

// livePreview shows a running transcript in the floating overlay while the
// hotkey is held. It is display-only: the pasted text still comes from the
// single full transcription in handleStopAndProcess.
type livePreview struct {
	ctx      context.Context
	ov       overlay.Overlay
	recorder *audio.Recorder
	stt      transcribe.Transcriber
	opts     transcribe.TranscribeOptions

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newLivePreview(ctx context.Context, ov overlay.Overlay, recorder *audio.Recorder, stt transcribe.Transcriber, opts transcribe.TranscribeOptions) *livePreview {
	return &livePreview{ctx: ctx, ov: ov, recorder: recorder, stt: stt, opts: opts}
}

// start shows the overlay and begins polling the recorder. Call after the
// recorder has started.
func (p *livePreview) start() {
	p.stop()

	ctx, cancel := context.WithCancel(p.ctx)
	done := make(chan struct{})
	p.mu.Lock()
	p.cancel, p.done = cancel, done
	p.mu.Unlock()

	p.ov.SetStatus(overlay.StatusListening)
	p.ov.Show()
	go p.loop(ctx, done)
}

// stop ends polling and waits for an in-flight pass, so a stale partial can't
// land on the overlay after the caller moves on. The overlay stays visible.
func (p *livePreview) stop() {
	p.halt()()
}

// halt ends polling and returns a func that waits for the in-flight pass.
// Split from the wait because Parakeet's decode ignores ctx: on key release
// the recorder must stop first, or audio keeps recording until the pass ends.
func (p *livePreview) halt() (wait func()) {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()

	if cancel == nil {
		return func() {}
	}
	cancel()
	return func() { <-done }
}

func (p *livePreview) loop(ctx context.Context, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(livePreviewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		wav, err := p.recorder.Snapshot()
		if errors.Is(err, audio.ErrNotRecording) {
			// Stopped without going through handleStopAndProcess (pause,
			// max-duration timer), so nobody else will hide the overlay.
			// A halted preview is mid-release; that caller owns the overlay.
			if ctx.Err() == nil {
				p.ov.Hide()
			}
			return
		}
		if err != nil {
			continue
		}

		text, err := p.stt.Transcribe(ctx, wav, p.opts)
		if err != nil || ctx.Err() != nil {
			continue
		}
		if text = strings.TrimSpace(text); text != "" && !isBlankAudio(text) {
			p.ov.UpdateText(text)
		}
	}
}
