//go:build darwin

// Package ui implements the menubar status item shown when vox is running.
// It owns the NSApp main run loop, which also drives the CGEventTap
// registered by internal/hotkey, so all UI calls must originate from a
// single process and Init must be called from the main goroutine before
// any other UI call.
package ui

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

void uiInit(const char *hotkeyLabel);
void uiSetSymbol(const char *name);
void uiSetStatusLine(const char *text);
void uiSetLastText(const char *text);
void uiSetHotkeyPresets(const char **specs, const char **labels, int count, const char *current);
void uiSetHotkeyCheckmark(const char *spec);
void uiSetHotkeyLabel(const char *label);
void uiSetModelPresets(const char **ids, const char **labels, const int *installed, int count, const char *current);
void uiSetModelCheckmark(const char *id);
void uiSetModelMenuEnabled(int on);
void uiSetPaused(int on);
void uiSetMode(int holdToTalk);
void uiSetSoundsEnabled(int on);
void uiSetAutoPaste(int on);
void uiSetAIPostProcess(int on);
void uiSetPromptMode(int on);
void uiSetVoiceCommands(int on);
void uiSetContextAware(int on);
void uiRun(void);
void uiQuit(void);
*/
import "C"

import (
	"unsafe"

	"golang.design/x/mainthread"
)

// State enumerates the UI states reflected in the menubar icon and status
// line.
type State int

const (
	// StateIdle: vox is running but no recording is in progress.
	StateIdle State = iota
	// StateRecording: the user is currently dictating.
	StateRecording
	// StateTranscribing: audio has been captured and is being sent to whisper.
	StateTranscribing
)

var (
	quitCh          = make(chan struct{}, 1)
	showLogCh       = make(chan struct{}, 1)
	hotkeyCh        = make(chan string, 1)
	modelCh         = make(chan string, 1)
	pauseCh         = make(chan bool, 1)
	modeCh          = make(chan bool, 1) // true = hold-to-talk, false = toggle
	soundsCh        = make(chan bool, 1)
	autoPasteCh     = make(chan bool, 1)
	aiPostProcessCh = make(chan bool, 1)
	promptModeCh    = make(chan bool, 1)
	voiceCommandsCh = make(chan bool, 1)
	contextAwareCh  = make(chan bool, 1)
)

// HotkeyPreset describes a selectable hotkey in the "Change Hotkey" submenu.
// Spec is the raw VOX_HOTKEY-style string (e.g. "option+space"); Label is
// what the menu shows the user (e.g. "Option+Space").
type HotkeyPreset struct {
	Spec  string
	Label string
}

// ModelPreset describes one selectable whisper model in the menubar.
type ModelPreset struct {
	ID        string
	Label     string
	Installed bool
}

// Init creates the NSStatusItem and prepares the NSApp activation policy.
// Cocoa requires NSStatusItem (and its underlying NSStatusBarWindow) to be
// created on the main OS thread, so we marshal the cgo call there via
// mainthread.Call.
//
// Must be called before Run. Safe to call from any goroutine — internally
// blocks until the main thread finishes the setup.
//
// hotkeyLabel is shown in the menu (e.g. "Option+Space") so users have a
// quick reminder of how to trigger dictation.
func Init(hotkeyLabel string) {
	mainthread.Call(func() {
		c := C.CString(hotkeyLabel)
		defer C.free(unsafe.Pointer(c))
		C.uiInit(c)
	})
}

// SetState updates the menubar icon and status line to reflect the given
// state. Safe to call from any goroutine.
func SetState(state State) {
	switch state {
	case StateIdle:
		setSymbol("waveform.circle")
		setStatusLine("Status: Idle")
	case StateRecording:
		setSymbol("waveform.circle.fill")
		setStatusLine("Status: Recording…")
	case StateTranscribing:
		setSymbol("arrow.trianglehead.2.clockwise.rotate.90.circle.fill")
		setStatusLine("Status: Transcribing…")
	}
}

// SetLastText shows the most recent transcription in the menubar dropdown.
// Pass an empty string to hide the row.
func SetLastText(text string) {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.uiSetLastText(c)
}

// SetStatusLine overrides the first disabled status row in the menu.
func SetStatusLine(text string) {
	setStatusLine(text)
}

// Run blocks the calling goroutine while NSApp's main event loop runs on the
// OS main thread. Cocoa requires [NSApp run] to be invoked on the main
// thread; we marshal the cgo call there via mainthread.Call. Returns when
// Quit is called or the user chooses Quit Vox in the menu.
//
// Safe to call from any goroutine.
func Run() {
	mainthread.Call(func() {
		C.uiRun()
	})
}

// Quit asks NSApp to terminate, causing Run to return.
func Quit() {
	C.uiQuit()
}

// OnQuit returns a channel that receives a value when the user clicks
// "Quit Vox" in the menubar.
func OnQuit() <-chan struct{} { return quitCh }

// OnShowLog returns a channel that receives a value when the user clicks
// "Show Log…" in the menubar.
func OnShowLog() <-chan struct{} { return showLogCh }

// OnHotkeyChange returns a channel that receives the hotkey spec
// (e.g. "option+space") the user picked from the "Change Hotkey" submenu.
func OnHotkeyChange() <-chan string { return hotkeyCh }

// OnModelChange returns a channel that receives the model ID (e.g. "base.en")
// picked from the "Whisper Model" submenu.
func OnModelChange() <-chan string { return modelCh }

// SetHotkeyPresets populates the "Change Hotkey" submenu. current is the
// spec that should display a checkmark on init (empty = no checkmark).
func SetHotkeyPresets(presets []HotkeyPreset, current string) {
	if len(presets) == 0 {
		return
	}
	specs := make([]*C.char, len(presets))
	labels := make([]*C.char, len(presets))
	for i, p := range presets {
		specs[i] = C.CString(p.Spec)
		labels[i] = C.CString(p.Label)
	}
	defer func() {
		for i := range specs {
			C.free(unsafe.Pointer(specs[i]))
			C.free(unsafe.Pointer(labels[i]))
		}
	}()
	curr := C.CString(current)
	defer C.free(unsafe.Pointer(curr))
	C.uiSetHotkeyPresets(
		(**C.char)(unsafe.Pointer(&specs[0])),
		(**C.char)(unsafe.Pointer(&labels[0])),
		C.int(len(presets)),
		curr,
	)
}

// SetHotkeyCheckmark moves the checkmark in the "Change Hotkey" submenu to
// the item whose spec matches.
func SetHotkeyCheckmark(spec string) {
	c := C.CString(spec)
	defer C.free(unsafe.Pointer(c))
	C.uiSetHotkeyCheckmark(c)
}

// SetHotkeyLabel updates the "Hotkey: X" info row in the main menu.
func SetHotkeyLabel(label string) {
	c := C.CString(label)
	defer C.free(unsafe.Pointer(c))
	C.uiSetHotkeyLabel(c)
}

// SetModelPresets populates the "Whisper Model" submenu.
func SetModelPresets(presets []ModelPreset, current string) {
	if len(presets) == 0 {
		return
	}
	ids := make([]*C.char, len(presets))
	labels := make([]*C.char, len(presets))
	installed := make([]C.int, len(presets))
	for i, p := range presets {
		ids[i] = C.CString(p.ID)
		labels[i] = C.CString(p.Label)
		installed[i] = boolToC(p.Installed)
	}
	defer func() {
		for i := range ids {
			C.free(unsafe.Pointer(ids[i]))
			C.free(unsafe.Pointer(labels[i]))
		}
	}()
	curr := C.CString(current)
	defer C.free(unsafe.Pointer(curr))
	C.uiSetModelPresets(
		(**C.char)(unsafe.Pointer(&ids[0])),
		(**C.char)(unsafe.Pointer(&labels[0])),
		(*C.int)(unsafe.Pointer(&installed[0])),
		C.int(len(presets)),
		curr,
	)
}

// SetModelCheckmark moves the checkmark in the "Whisper Model" submenu.
func SetModelCheckmark(id string) {
	c := C.CString(id)
	defer C.free(unsafe.Pointer(c))
	C.uiSetModelCheckmark(c)
}

// SetModelMenuEnabled toggles whether the model submenu can be clicked.
func SetModelMenuEnabled(on bool) { C.uiSetModelMenuEnabled(boolToC(on)) }

// SetPaused updates the "Pause Vox" / "Resume Vox" menu item and dims the
// menubar icon (renders as waveform.slash) so vox's disabled state is
// obvious at a glance. Idempotent.
func SetPaused(on bool) { C.uiSetPaused(boolToC(on)) }

// SetMode updates the Mode submenu radio: true = "Hold to Talk", false = "Toggle".
func SetMode(holdToTalk bool) { C.uiSetMode(boolToC(holdToTalk)) }

// SetSoundsEnabled updates the "Play sounds" checkbox.
func SetSoundsEnabled(on bool) { C.uiSetSoundsEnabled(boolToC(on)) }

// SetAutoPaste updates the "Auto-paste" checkbox.
func SetAutoPaste(on bool) { C.uiSetAutoPaste(boolToC(on)) }

// OnPauseToggle returns a channel that receives the new paused state when
// the user clicks "Pause Vox" / "Resume Vox".
func OnPauseToggle() <-chan bool { return pauseCh }

// OnModeChange returns a channel that receives true for hold-to-talk and
// false for toggle when the user picks from the Mode submenu.
func OnModeChange() <-chan bool { return modeCh }

// OnSoundsToggle returns a channel that receives the new state.
func OnSoundsToggle() <-chan bool { return soundsCh }

// OnAutoPasteToggle returns a channel that receives the new state.
func OnAutoPasteToggle() <-chan bool { return autoPasteCh }

// --- AI Feature Toggles ---

// SetAIPostProcess updates the "AI post-processing" checkbox.
func SetAIPostProcess(on bool) { C.uiSetAIPostProcess(boolToC(on)) }

// SetPromptMode updates the "Prompt mode" checkbox.
func SetPromptMode(on bool) { C.uiSetPromptMode(boolToC(on)) }

// SetVoiceCommands updates the "Voice commands" checkbox.
func SetVoiceCommands(on bool) { C.uiSetVoiceCommands(boolToC(on)) }

// SetContextAware updates the "Context-aware" checkbox.
func SetContextAware(on bool) { C.uiSetContextAware(boolToC(on)) }

// OnAIPostProcessToggle returns a channel that receives the new state.
func OnAIPostProcessToggle() <-chan bool { return aiPostProcessCh }

// OnPromptModeToggle returns a channel that receives the new state.
func OnPromptModeToggle() <-chan bool { return promptModeCh }

// OnVoiceCommandsToggle returns a channel that receives the new state.
func OnVoiceCommandsToggle() <-chan bool { return voiceCommandsCh }

// OnContextAwareToggle returns a channel that receives the new state.
func OnContextAwareToggle() <-chan bool { return contextAwareCh }

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

func setSymbol(s string) {
	c := C.CString(s)
	defer C.free(unsafe.Pointer(c))
	C.uiSetSymbol(c)
}

func setStatusLine(s string) {
	c := C.CString(s)
	defer C.free(unsafe.Pointer(c))
	C.uiSetStatusLine(c)
}

//export onQuitClicked
func onQuitClicked() {
	select {
	case quitCh <- struct{}{}:
	default:
	}
}

//export onShowLogClicked
func onShowLogClicked() {
	select {
	case showLogCh <- struct{}{}:
	default:
	}
}

//export onHotkeyChosen
func onHotkeyChosen(spec *C.char) {
	s := C.GoString(spec)
	select {
	case hotkeyCh <- s:
	default:
	}
}

//export onModelChosen
func onModelChosen(id *C.char) {
	s := C.GoString(id)
	select {
	case modelCh <- s:
	default:
	}
}

//export onPauseToggled
func onPauseToggled(on C.int) { send(pauseCh, on != 0) }

//export onModeChanged
func onModeChanged(holdToTalk C.int) { send(modeCh, holdToTalk != 0) }

//export onSoundsToggled
func onSoundsToggled(on C.int) { send(soundsCh, on != 0) }

//export onAutoPasteToggled
func onAutoPasteToggled(on C.int) { send(autoPasteCh, on != 0) }

//export onAIPostProcessToggled
func onAIPostProcessToggled(on C.int) { send(aiPostProcessCh, on != 0) }

//export onPromptModeToggled
func onPromptModeToggled(on C.int) { send(promptModeCh, on != 0) }

//export onVoiceCommandsToggled
func onVoiceCommandsToggled(on C.int) { send(voiceCommandsCh, on != 0) }

//export onContextAwareToggled
func onContextAwareToggled(on C.int) { send(contextAwareCh, on != 0) }

func send(ch chan bool, v bool) {
	select {
	case ch <- v:
	default:
	}
}
