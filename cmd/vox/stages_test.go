package main

import (
	"context"
	"testing"

	"vox/internal/pipeline"
	"vox/internal/transcribe"
)

type fakeTranscriber struct {
	text string
	err  error
	got  []byte
}

func (f *fakeTranscriber) Transcribe(_ context.Context, wav []byte, _ transcribe.TranscribeOptions) (string, error) {
	f.got = wav
	return f.text, f.err
}

func TestTranscribeStageUsesInterface(t *testing.T) {
	ft := &fakeTranscriber{text: "hello world"}
	stage := transcribeStage(ft, transcribe.TranscribeOptions{})

	r := &pipeline.Result{RawAudio: []byte("fake-wav")}
	if err := stage(context.Background(), r); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if r.RawText != "hello world" {
		t.Errorf("RawText = %q, want %q", r.RawText, "hello world")
	}
	if r.OutputText != "hello world" {
		t.Errorf("OutputText = %q, want %q", r.OutputText, "hello world")
	}
	if string(ft.got) != "fake-wav" {
		t.Errorf("transcriber got %q, want %q", ft.got, "fake-wav")
	}
}

func TestFilterBlankStage_EmptyText(t *testing.T) {
	stage := filterBlankStage()
	r := &pipeline.Result{RawText: ""}
	if err := stage(context.Background(), r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Cancelled {
		t.Fatal("expected Cancelled=true for empty RawText")
	}
}

func TestFilterBlankStage_BlankAudioHallucination(t *testing.T) {
	for _, text := range []string{
		"[BLANK_AUDIO]",
		"[blank audio]",
		"(BLANK_AUDIO)",
		"(blank audio)",
		" [BLANK_AUDIO] ",
	} {
		t.Run(text, func(t *testing.T) {
			stage := filterBlankStage()
			r := &pipeline.Result{RawText: text}
			if err := stage(context.Background(), r); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !r.Cancelled {
				t.Fatalf("expected Cancelled=true for %q", text)
			}
		})
	}
}

func TestFilterBlankStage_ValidText(t *testing.T) {
	stage := filterBlankStage()
	r := &pipeline.Result{RawText: "hello world"}
	if err := stage(context.Background(), r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Cancelled {
		t.Fatal("expected Cancelled=false for valid text")
	}
}

func TestFilterBlankStage_ChecksRawTextNotOutputText(t *testing.T) {
	// Verify the stage checks RawText (not OutputText) for blank detection.
	stage := filterBlankStage()
	r := &pipeline.Result{
		RawText:    "hello world",
		OutputText: "", // OutputText is empty but RawText is valid
	}
	if err := stage(context.Background(), r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Cancelled {
		t.Fatal("filterBlankStage should check RawText, not OutputText")
	}
}

func TestIsBlankAudio(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"[BLANK_AUDIO]", true},
		{"[blank audio]", true},
		{"(BLANK_AUDIO)", true},
		{"(blank audio)", true},
		{" [BLANK_AUDIO] ", true},
		{"blank_audio", true},
		{"blank audio", true},
		{"hello world", false},
		{"", false},
		{"BLANK", false},
		{"audio", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isBlankAudio(tt.input)
			if got != tt.want {
				t.Errorf("isBlankAudio(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
