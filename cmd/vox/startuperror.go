package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"

	"vox/internal/ui"
)

// quitWithStartupError tells the user why Vox is not going to run, then exits
// non-zero.
//
// The stderr text is the whole story for a terminal run. A bundle launched
// from Finder has no terminal, and Vox is an LSUIElement app with no Dock
// icon or window, so the same text also goes to a modal alert — otherwise a
// double-click just appears to do nothing.
//
// settingsPane may be empty when no System Settings pane would help.
func quitWithStartupError(title, detail, settingsPane string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", title)
	for _, line := range strings.Split(detail, "\n") {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	_, inAppBundle := appBundlePath()
	if shouldAlertOnStartupError(inAppBundle, stderrIsTerminal()) {
		ui.ShowStartupError(title, detail, settingsPane)
	}
	os.Exit(1)
}

// shouldAlertOnStartupError reports whether the stderr text will go unread,
// which is when the alert is the only way to reach the user.
//
// Both conditions matter. Outside a bundle Cocoa has no registered app to
// attach a modal to and NSAlert returns without ever drawing, so a bare
// binary must stay text-only however it was redirected.
func shouldAlertOnStartupError(inAppBundle, stderrOnTerminal bool) bool {
	return inAppBundle && !stderrOnTerminal
}

// stderrIsTerminal reports whether a terminal is reading Vox's error output.
// False for a Finder or launchd start, which hands the app /dev/null, and for
// `make start`, which redirects to a log file.
//
// Asks whether the descriptor is a terminal, not whether it is a character
// device: /dev/null is a character device too, and counting it as a terminal
// is what let a Finder launch fail with its reason written nowhere.
func stderrIsTerminal() bool {
	_, err := unix.IoctlGetTermios(int(os.Stderr.Fd()), unix.TIOCGETA)
	return err == nil
}
