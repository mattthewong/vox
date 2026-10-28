//go:build darwin

package overlay

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

void overlayShow(void);
void overlayHide(void);
void overlayUpdateText(const char *text);
void overlaySetStatus(const char *status);
void overlayInit(void);
void overlayClose(void);
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"unicode/utf8"
	"unsafe"
)

// Compile-time interface check.
var _ Overlay = (*DarwinOverlay)(nil)

var (
	darwinOnce     sync.Once
	darwinInstance *DarwinOverlay
)

// DarwinOverlay is a macOS floating window overlay for real-time transcription.
// It uses a borderless, always-on-top NSWindow positioned at the bottom-center
// of the screen.
//
// Every AppKit call is dispatched asynchronously onto the main queue from the
// C side, the same way the menubar UI does it. mainthread.Call would deadlock
// here: once ui.Run starts [NSApp run], the main thread never returns to
// service mainthread's call queue.
//
// DarwinOverlay is a singleton — multiple calls to New() return the same instance.
type DarwinOverlay struct {
	closed atomic.Bool
}

// New creates the macOS overlay window. It is safe to call from any goroutine
// once ui.Init has created NSApp; the window is built on the main queue.
// Subsequent calls return the same singleton instance.
func New() *DarwinOverlay {
	darwinOnce.Do(func() {
		C.overlayInit()
		darwinInstance = &DarwinOverlay{}
	})
	return darwinInstance
}

func (o *DarwinOverlay) Show() {
	if o.closed.Load() {
		return
	}
	C.overlayShow()
}

func (o *DarwinOverlay) Hide() {
	if o.closed.Load() {
		return
	}
	C.overlayHide()
}

func (o *DarwinOverlay) UpdateText(text string) {
	if o.closed.Load() {
		return
	}
	if !utf8.ValidString(text) {
		text = "[invalid text]"
	}
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.overlayUpdateText(cs)
}

func (o *DarwinOverlay) SetStatus(status Status) {
	if o.closed.Load() {
		return
	}
	cs := C.CString(status.String())
	defer C.free(unsafe.Pointer(cs))
	C.overlaySetStatus(cs)
}

func (o *DarwinOverlay) Close() {
	if o.closed.Swap(true) {
		return
	}
	C.overlayClose()
}
