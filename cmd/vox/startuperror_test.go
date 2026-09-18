package main

import (
	"os"
	"path/filepath"
	"testing"
)

// withStderr swaps os.Stderr for the duration of a test.
func withStderr(t *testing.T, f *os.File) {
	t.Helper()
	saved := os.Stderr
	os.Stderr = f
	t.Cleanup(func() { os.Stderr = saved })
}

// A Finder or launchd launch hands Vox a regular file for stderr, which is
// the case that has to reach the alert.
func TestStderrIsNotTerminalForRegularFile(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "vox.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	withStderr(t, f)

	if stderrIsTerminal() {
		t.Error("stderrIsTerminal() = true for a regular file; the startup alert would be skipped on a Finder launch")
	}
}

func TestStderrIsNotTerminalForPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	withStderr(t, w)

	if stderrIsTerminal() {
		t.Error("stderrIsTerminal() = true for a pipe")
	}
}

func TestStderrIsNotTerminalWhenClosed(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "closed.log"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	withStderr(t, f)

	// Stat on a closed descriptor errors; a startup failure must still be
	// reported somewhere rather than silently suppressed.
	if stderrIsTerminal() {
		t.Error("stderrIsTerminal() = true for a closed file")
	}
}

// Finder and launchd hand an app /dev/null for stderr. It is a character
// device but not a terminal, and mistaking the two is what made a failed
// Finder launch report its reason nowhere at all.
func TestStderrIsNotTerminalForDevNull(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	withStderr(t, f)

	if stderrIsTerminal() {
		t.Error("stderrIsTerminal() = true for /dev/null; a Finder launch would fail silently")
	}
}

// A terminal run must not raise a modal dialog in front of the user, so the
// terminal branch has to stay reachable.
func TestStderrIsTerminalForTTY(t *testing.T) {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		t.Skip("no controlling terminal")
	}
	defer f.Close()
	withStderr(t, f)

	if !stderrIsTerminal() {
		t.Error("stderrIsTerminal() = false for a tty")
	}
}

// NSAlert returns without drawing when the process is not a registered app,
// so a redirected `go run ./cmd/vox` must stay text-only rather than silently
// consuming a button response it never showed.
func TestNoStartupAlertOutsideAppBundle(t *testing.T) {
	if _, bundled := appBundlePath(); bundled {
		t.Skip("test binary resolved to an .app bundle")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "vox.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	withStderr(t, f)

	if shouldAlertOnStartupError() {
		t.Error("shouldAlertOnStartupError() = true for a bare binary")
	}
}
