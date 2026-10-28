package main

import (
	"context"
	"sync"
	"testing"

	"vox/internal/audio"
	"vox/internal/overlay"
	"vox/internal/transcribe"
)

type fakeOverlay struct {
	mu     sync.Mutex
	shown  bool
	status overlay.Status
}

func (f *fakeOverlay) Show()                      { f.mu.Lock(); f.shown = true; f.mu.Unlock() }
func (f *fakeOverlay) Hide()                      { f.mu.Lock(); f.shown = false; f.mu.Unlock() }
func (f *fakeOverlay) UpdateText(string)          {}
func (f *fakeOverlay) SetStatus(s overlay.Status) { f.mu.Lock(); f.status = s; f.mu.Unlock() }
func (f *fakeOverlay) Close()                     {}

type nopTranscriber struct{}

func (nopTranscriber) Transcribe(context.Context, []byte, transcribe.TranscribeOptions) (string, error) {
	return "", nil
}

func TestLivePreviewStartStop(t *testing.T) {
	rec, err := audio.NewRecorder()
	if err != nil {
		t.Skipf("no recording tool: %v", err)
	}
	ov := &fakeOverlay{}
	p := newLivePreview(context.Background(), ov, rec, nopTranscriber{}, transcribe.TranscribeOptions{})

	p.start()
	if !ov.shown || ov.status != overlay.StatusListening {
		t.Fatalf("start: shown=%v status=%v, want shown listening", ov.shown, ov.status)
	}

	// Restarting and double-stopping must not block or panic.
	p.start()
	p.stop()
	p.stop()
}
