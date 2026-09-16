//go:build !darwin

package hotkey

import "errors"

// Listener monitors for global hotkey triggers.
// This is a stub for non-darwin platforms.
type Listener struct {
	keydown chan struct{}
	keyup   chan struct{}
}

// NewListener creates a listener (stub on non-darwin platforms).
func NewListener(_ []Trigger) *Listener {
	return &Listener{
		keydown: make(chan struct{}, 1),
		keyup:   make(chan struct{}, 1),
	}
}

// CheckAccessibility always returns true on non-darwin platforms.
func CheckAccessibility() bool { return true }

// AccessibilityGranted always returns true on non-darwin platforms.
func AccessibilityGranted() bool { return true }

// Start returns an error on non-darwin platforms.
// It closes the keydown/keyup channels so any goroutine blocked on them
// unblocks cleanly instead of leaking.
func (l *Listener) Start() error {
	close(l.keydown)
	close(l.keyup)
	return errors.New("hotkey listener not supported on this platform (macOS only)")
}

// Keydown returns the keydown channel.
func (l *Listener) Keydown() <-chan struct{} { return l.keydown }

// Keyup returns the keyup channel.
func (l *Listener) Keyup() <-chan struct{} { return l.keyup }
