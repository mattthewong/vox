//go:build darwin

package hotkey

import "testing"

// TestRequestMicrophoneAccessSmoke verifies the CGo bridge runs without
// panicking. The result depends on the system's current permission state.
func TestRequestMicrophoneAccessSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping microphone permission test in short mode")
	}
	_ = RequestMicrophoneAccess()
}
