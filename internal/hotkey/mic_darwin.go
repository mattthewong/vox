//go:build darwin

package hotkey

/*
#cgo LDFLAGS: -framework AVFoundation

int requestMicrophoneAccess(void);
*/
import "C"

// RequestMicrophoneAccess requests microphone access from macOS.
// Shows a system permission dialog if access has not yet been determined.
// Returns true if access is granted.
func RequestMicrophoneAccess() bool {
	return C.requestMicrophoneAccess() == 1
}
