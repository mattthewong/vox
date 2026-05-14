//go:build darwin

package notify

import "testing"

func TestDarwinNotifierInterface(t *testing.T) {
	var _ Notifier = (*DarwinNotifier)(nil)
}

func TestNewReturnsNotifier(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New() returned nil")
	}
}

func TestSendSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping notification smoke test in short mode")
	}
	n := NewDarwinNotifier()
	n.Send("Vox Test", "This is a test notification from vox.")
}
