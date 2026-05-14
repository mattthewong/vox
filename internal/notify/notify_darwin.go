//go:build darwin

package notify

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>

void notifySend(const char *title, const char *body);
*/
import "C"

import (
	"unicode/utf8"
	"unsafe"
)

// DarwinNotifier sends macOS notifications via NSUserNotification.
type DarwinNotifier struct{}

// NewDarwinNotifier creates a macOS notifier.
func NewDarwinNotifier() *DarwinNotifier { return &DarwinNotifier{} }

// New returns a platform-specific Notifier (macOS).
func New() Notifier { return NewDarwinNotifier() }

func (n *DarwinNotifier) Send(title, body string) {
	if title == "" && body == "" {
		return
	}
	if !utf8.ValidString(title) {
		title = "[invalid text]"
	}
	if !utf8.ValidString(body) {
		body = "[invalid text]"
	}
	ct := C.CString(title)
	cb := C.CString(body)
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cb))
	C.notifySend(ct, cb)
}
