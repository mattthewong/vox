//go:build !darwin

package hotkey

import (
	"testing"
)

func TestStubStartReturnsError(t *testing.T) {
	l := NewListener(nil)
	if err := l.Start(); err == nil {
		t.Fatal("stub Start() must return a non-nil error")
	}
}

func TestStubStartClosesChannels(t *testing.T) {
	l := NewListener(nil)
	_ = l.Start()

	// After Start(), channels should be closed — reads return immediately.
	select {
	case _, ok := <-l.Keydown():
		if ok {
			t.Error("Keydown channel should be closed after Start()")
		}
	default:
		t.Error("Keydown channel should be readable (closed) after Start()")
	}

	select {
	case _, ok := <-l.Keyup():
		if ok {
			t.Error("Keyup channel should be closed after Start()")
		}
	default:
		t.Error("Keyup channel should be readable (closed) after Start()")
	}
}

func TestStubCheckAccessibilityReturnsTrue(t *testing.T) {
	if !CheckAccessibility() {
		t.Error("stub CheckAccessibility() must return true")
	}
}

func TestStubAccessibilityGrantedReturnsTrue(t *testing.T) {
	if !AccessibilityGranted() {
		t.Error("stub AccessibilityGranted() must return true")
	}
}

func TestStubRequestMicrophoneAccessReturnsTrue(t *testing.T) {
	if !RequestMicrophoneAccess() {
		t.Error("stub RequestMicrophoneAccess() must return true")
	}
}

func TestStubMicrophoneAuthorizedReturnsTrue(t *testing.T) {
	if !MicrophoneAuthorized() {
		t.Error("stub MicrophoneAuthorized() must return true")
	}
}
