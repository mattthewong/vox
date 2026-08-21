package transcribe

import (
	"context"
	"testing"
)

// stubTranscriber is a minimal Transcriber used to prove the interface is
// satisfiable by something other than *Client.
type stubTranscriber struct{ text string }

func (s stubTranscriber) Transcribe(_ context.Context, _ []byte, _ TranscribeOptions) (string, error) {
	return s.text, nil
}

func TestClientSatisfiesTranscriber(t *testing.T) {
	var _ Transcriber = (*Client)(nil)
}

func TestStubSatisfiesTranscriber(t *testing.T) {
	var tr Transcriber = stubTranscriber{text: "hello"}
	got, err := tr.Transcribe(context.Background(), []byte("ignored"), TranscribeOptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}
