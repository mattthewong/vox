//go:build darwin

package hotkey

/*
#cgo LDFLAGS: -framework AVFoundation

int requestMicrophoneAccess(void);
int microphoneAuthorized(void);
*/
import "C"

// RequestMicrophoneAccess requests microphone access from macOS.
// Shows a system permission dialog if access has not yet been determined.
// Returns true if access is granted.
func RequestMicrophoneAccess() bool {
	return C.requestMicrophoneAccess() == 1
}

// MicrophoneAuthorized reports whether access is already granted. Unlike
// RequestMicrophoneAccess it never shows a prompt, so it is safe to call
// from read-only diagnostics.
func MicrophoneAuthorized() bool {
	return C.microphoneAuthorized() == 1
}
