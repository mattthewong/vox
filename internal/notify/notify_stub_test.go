//go:build !darwin

package notify

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestStubNotifierInterface(t *testing.T) {
	var _ Notifier = (*StubNotifier)(nil)
}

func TestNewReturnsNotifier(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New() returned nil")
	}
}

func TestStubSendWritesToStderr(t *testing.T) {
	// Capture stderr output.
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	n := NewStubNotifier()
	n.Send("Vox", "test message")

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	got := buf.String()

	if !strings.Contains(got, "[notify] Vox: test message") {
		t.Errorf("expected stderr to contain notification, got: %q", got)
	}
}
