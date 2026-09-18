package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// tccDatabasePath is where macOS records Accessibility grants. It is
// system-wide, so reading it requires the calling process to have Full Disk
// Access. VOX_TCC_DB_PATH overrides it for tests.
func tccDatabasePath() string {
	if p := os.Getenv("VOX_TCC_DB_PATH"); p != "" {
		return p
	}
	return "/Library/Application Support/com.apple.TCC/TCC.db"
}

// tccAuthValueAllowed is the auth_value macOS writes for an allowed grant.
const tccAuthValueAllowed = 2

// appGrantState is what macOS has recorded for Vox's own Accessibility
// permission, as opposed to what the running process can currently do.
type appGrantState int

const (
	// grantUnreadable means the TCC database could not be read, which on a
	// healthy system means the caller lacks Full Disk Access.
	grantUnreadable appGrantState = iota
	// grantAbsent means macOS has no Accessibility record for Vox at all.
	grantAbsent
	// grantDenied means the record exists and withholds permission.
	grantDenied
	// grantStaleSignature means permission was granted to a differently
	// signed build. System Settings still shows Vox as enabled, but macOS
	// will not honour the grant and Vox prompts on every launch.
	grantStaleSignature
	// grantActive means the record allows Vox and matches this build.
	grantActive
)

// recordedAccessibilityGrant reports what macOS has on record for id's
// Accessibility permission and whether appPath still satisfies the signature
// that grant is bound to. Distinct from hotkey.AccessibilityGranted, which
// asks what the running process may do right now and answers for whichever
// app macOS credits with the check.
func recordedAccessibilityGrant(appPath, id string) (appGrantState, error) {
	authValue, requirement, err := tccAccessibilityRecord(tccDatabasePath(), id)
	if err != nil {
		return grantUnreadable, err
	}
	switch {
	case authValue == nil:
		return grantAbsent, nil
	case *authValue != tccAuthValueAllowed:
		return grantDenied, nil
	}
	matches, err := bundleSatisfiesRequirement(appPath, requirement)
	if err != nil {
		return grantUnreadable, err
	}
	if !matches {
		return grantStaleSignature, nil
	}
	return grantActive, nil
}

// tccAccessibilityRecord reads the Accessibility row for id. A nil auth value
// means there is no row. The requirement is the code requirement the grant is
// bound to, which macOS re-checks on every launch.
func tccAccessibilityRecord(dbPath, id string) (*int, []byte, error) {
	query := fmt.Sprintf(
		"select auth_value, hex(csreq) from access "+
			"where service='kTCCServiceAccessibility' and client=%s;",
		sqliteQuote(id),
	)
	out, err := exec.Command("sqlite3", "-readonly", dbPath, query).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dbPath, err)
	}
	return parseTCCAccessibilityRow(string(out))
}

// parseTCCAccessibilityRow decodes one "auth_value|hex(csreq)" line. Empty
// output means no grant, which is not an error.
func parseTCCAccessibilityRow(out string) (*int, []byte, error) {
	line := strings.TrimSpace(out)
	if line == "" {
		return nil, nil, nil
	}
	// More than one row would mean two clients matched an exact-match query.
	line = strings.SplitN(line, "\n", 2)[0]
	fields := strings.SplitN(line, "|", 2)
	if len(fields) != 2 {
		return nil, nil, fmt.Errorf("unexpected TCC row %q", line)
	}
	authValue, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return nil, nil, fmt.Errorf("unexpected auth_value in %q: %w", line, err)
	}
	requirement, err := hex.DecodeString(strings.TrimSpace(fields[1]))
	if err != nil {
		return nil, nil, fmt.Errorf("unexpected csreq in %q: %w", line, err)
	}
	return &authValue, requirement, nil
}

// sqliteQuote renders s as a SQL string literal.
func sqliteQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// bundleSatisfiesRequirement asks codesign whether appPath meets the compiled
// code requirement stored with a TCC grant — the same question macOS asks
// before honouring that grant. An empty requirement is treated as satisfied,
// since macOS then falls back to the bundle ID alone.
func bundleSatisfiesRequirement(appPath string, requirement []byte) (bool, error) {
	if len(requirement) == 0 {
		return true, nil
	}
	f, err := os.CreateTemp("", "vox-csreq-*.bin")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(requirement); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	// codesign exits non-zero both for "does not satisfy" and for a broken
	// bundle; either way macOS will not honour the grant, so both are a
	// mismatch rather than an error.
	err = exec.Command("codesign", "--verify", "-R", f.Name(), appPath).Run()
	return err == nil, nil
}

// appBundlePath returns the .app directory containing this executable, or
// false when Vox is running as a bare binary and so has no bundle identity
// for macOS to grant permissions to.
func appBundlePath() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	// .../Vox.app/Contents/MacOS/vox
	dir := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(dir) != ".app" {
		return "", false
	}
	return dir, true
}

// maxAncestorHops bounds the walk up the process tree.
const maxAncestorHops = 16

// launchingApp finds the app Vox was launched from, which is the app macOS
// credits with any permission check Vox makes. When that app holds
// Accessibility — as terminals typically do — the check succeeds regardless
// of what Vox itself was granted, so a runtime check says nothing about Vox's
// own grant. The name is empty when the launching app cannot be identified,
// and launched is false when launchd started Vox directly, as Finder does.
func launchingApp() (name string, launched bool) {
	pid := os.Getpid()
	for hop := 0; hop < maxAncestorHops; hop++ {
		parent, ok := parentPID(pid)
		if !ok || parent <= 1 {
			return "", hop > 0
		}
		path, ok := executablePath(parent)
		if !ok {
			return "", true
		}
		if app, inBundle := appNameOfExecutable(path); inBundle {
			return app, true
		}
		pid = parent
	}
	return "", true
}

// appNameOfExecutable names the .app bundle an executable lives in, as the
// user would see it in System Settings.
func appNameOfExecutable(path string) (string, bool) {
	for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if filepath.Ext(dir) == ".app" {
			return strings.TrimSuffix(filepath.Base(dir), ".app"), true
		}
	}
	return "", false
}

// parentPID reads a process's parent from ps.
func parentPID(pid int) (int, bool) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// executablePath reads the path a process was started from.
func executablePath(pid int) (string, bool) {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", false
	}
	return path, true
}
