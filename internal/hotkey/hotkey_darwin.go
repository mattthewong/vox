//go:build darwin

package hotkey

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation

int startEventTap(void);
int checkAccessibility(int prompt);
*/
import "C"

import (
	"errors"
	"sync"
)

// CGEvent modifier flag bitmasks.
const (
	FlagShift   uint64 = 0x20000
	FlagControl uint64 = 0x40000
	FlagOption  uint64 = 0x80000
	FlagCommand uint64 = 0x100000
	FlagFn      uint64 = 0x800000
)

// CGEvent type constants.
const (
	eventFlagsChanged uint32 = 12
	eventKeyDown      uint32 = 10
	eventKeyUp        uint32 = 11
)

// modMask covers the four standard modifier flags.
const modMask = FlagShift | FlagControl | FlagOption | FlagCommand

// Trigger defines a hotkey trigger condition.
type Trigger struct {
	// Modifiers is a bitmask of required modifier flags (FlagShift, FlagCommand, etc.).
	Modifiers uint64
	// Key is a macOS virtual keycode, or -1 for modifier-only triggers.
	Key int
	// Fn triggers on the Fn/Globe key.
	Fn bool
	// Label is a human-readable description (e.g. "Cmd+Shift", "Fn").
	Label string
}

var (
	global *Listener
	mu     sync.Mutex
)

// Listener monitors for global hotkey triggers via macOS CGEventTap.
type Listener struct {
	triggers       []Trigger
	active         bool
	matchedTrigger int
	keydown        chan struct{}
	keyup          chan struct{}
}

// NewListener creates a listener for the given triggers.
func NewListener(triggers []Trigger) *Listener {
	return &Listener{
		triggers:       triggers,
		matchedTrigger: -1,
		keydown:        make(chan struct{}, 1),
		keyup:          make(chan struct{}, 1),
	}
}

// CheckAccessibility returns true if the process has Accessibility permission.
func CheckAccessibility() bool {
	// Request macOS to show the Accessibility permission prompt if needed.
	return C.checkAccessibility(1) == 1
}

// Start begins listening for hotkey events. This function blocks forever
// (runs a CFRunLoop). It must be called from the main thread on macOS.
func (l *Listener) Start() error {
	mu.Lock()
	global = l
	mu.Unlock()

	ret := C.startEventTap()
	switch ret {
	case -1:
		return errors.New("failed to create event tap; grant Accessibility permission in System Settings > Privacy & Security > Accessibility")
	default:
		return nil
	}
}

// Keydown returns a channel that receives when a hotkey is pressed.
func (l *Listener) Keydown() <-chan struct{} { return l.keydown }

// Keyup returns a channel that receives when a hotkey is released.
func (l *Listener) Keyup() <-chan struct{} { return l.keyup }

//export goEventCallback
func goEventCallback(eventType uint32, flags uint64, keycode int64) int32 {
	mu.Lock()
	l := global
	mu.Unlock()
	if l == nil {
		return 0
	}

	switch eventType {
	case eventFlagsChanged:
		return l.handleFlagsChanged(flags)
	case eventKeyDown:
		return l.handleKeyDown(flags, keycode)
	case eventKeyUp:
		return l.handleKeyUp(keycode)
	}
	return 0
}

func (l *Listener) handleFlagsChanged(flags uint64) int32 {
	consume := int32(0)
	matched := false

	for i, t := range l.triggers {
		if t.Key >= 0 {
			continue // modifier+key triggers handled in keyDown/keyUp
		}

		if t.Fn {
			if flags&FlagFn != 0 {
				matched = true
				if !l.active {
					l.matchedTrigger = i
				}
				consume = 1 // consume fn to prevent emoji picker
				break
			}
		} else if t.Modifiers != 0 {
			activeModifiers := flags & modMask
			if activeModifiers == t.Modifiers {
				matched = true
				if !l.active {
					l.matchedTrigger = i
				}
				break
			}
		}
	}

	if matched && !l.active {
		l.active = true
		select {
		case l.keydown <- struct{}{}:
		default:
		}
	} else if !matched && l.active && l.matchedTrigger >= 0 && l.triggers[l.matchedTrigger].Key < 0 {
		l.active = false
		l.matchedTrigger = -1
		select {
		case l.keyup <- struct{}{}:
		default:
		}
	}

	return consume
}

func (l *Listener) handleKeyDown(flags uint64, keycode int64) int32 {
	// Check modifier+key triggers.
	for i, t := range l.triggers {
		if t.Key < 0 {
			continue
		}
		if int64(t.Key) == keycode && flags&modMask == t.Modifiers {
			if !l.active {
				l.active = true
				l.matchedTrigger = i
				select {
				case l.keydown <- struct{}{}:
				default:
				}
			}
			return 1 // consume
		}
	}

	// If a modifier-only trigger is active and a regular key is pressed,
	// the user is performing a keyboard shortcut, not dictating. Cancel.
	if l.active && l.matchedTrigger >= 0 && l.triggers[l.matchedTrigger].Key < 0 {
		l.active = false
		l.matchedTrigger = -1
		select {
		case l.keyup <- struct{}{}:
		default:
		}
	}

	return 0
}

func (l *Listener) handleKeyUp(keycode int64) int32 {
	if !l.active || l.matchedTrigger < 0 {
		return 0
	}
	t := l.triggers[l.matchedTrigger]
	if t.Key >= 0 && int64(t.Key) == keycode {
		l.active = false
		l.matchedTrigger = -1
		select {
		case l.keyup <- struct{}{}:
		default:
		}
		return 1 // consume
	}
	return 0
}
