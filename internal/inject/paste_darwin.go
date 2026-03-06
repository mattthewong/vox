//go:build darwin

package inject

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// simulateCmdV posts Cmd+V keyboard events to the system.
void simulateCmdV(void) {
    // keycode 9 = 'v' on macOS
    CGEventRef keyDown = CGEventCreateKeyboardEvent(NULL, 9, true);
    CGEventRef keyUp   = CGEventCreateKeyboardEvent(NULL, 9, false);

    CGEventSetFlags(keyDown, kCGEventFlagMaskCommand);
    CGEventSetFlags(keyUp, kCGEventFlagMaskCommand);

    CGEventPost(kCGHIDEventTap, keyDown);
    CGEventPost(kCGHIDEventTap, keyUp);

    CFRelease(keyDown);
    CFRelease(keyUp);
}
*/
import "C"

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// TypeText copies the given text to the system clipboard and simulates Cmd+V
// in the currently focused macOS application. Empty text is a no-op.
func TypeText(text string) error {
	if text == "" {
		return nil
	}

	if err := copyToClipboard(text); err != nil {
		return fmt.Errorf("inject: copy to clipboard: %w", err)
	}

	// Brief delay to ensure clipboard contents are committed.
	time.Sleep(50 * time.Millisecond)

	// Post Cmd+V via CGEvent (works regardless of which app is focused).
	C.simulateCmdV()

	return nil
}

// ClearClipboard writes an empty string to the system clipboard.
func ClearClipboard() error {
	return copyToClipboard("")
}

func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pbcopy: %w (output: %s)", err, string(output))
	}
	return nil
}
