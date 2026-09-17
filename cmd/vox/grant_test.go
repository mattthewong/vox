package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTCCAccessibilityRowNoGrant(t *testing.T) {
	authValue, requirement, err := parseTCCAccessibilityRow("\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if authValue != nil {
		t.Errorf("auth value = %d, want nil for an absent grant", *authValue)
	}
	if requirement != nil {
		t.Errorf("requirement = %x, want nil", requirement)
	}
}

func TestParseTCCAccessibilityRowAllowed(t *testing.T) {
	// Shape of the real row: auth_value, then the requirement as hex.
	authValue, requirement, err := parseTCCAccessibilityRow("2|FADE0C00DEADBEEF\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if authValue == nil || *authValue != tccAuthValueAllowed {
		t.Fatalf("auth value = %v, want %d", authValue, tccAuthValueAllowed)
	}
	if got := hex.EncodeToString(requirement); got != "fade0c00deadbeef" {
		t.Errorf("requirement = %s, want fade0c00deadbeef", got)
	}
}

func TestParseTCCAccessibilityRowRejectsGarbage(t *testing.T) {
	for _, row := range []string{"allowed|FADE0C00", "2", "2|nothex"} {
		if _, _, err := parseTCCAccessibilityRow(row); err == nil {
			t.Errorf("parse(%q) succeeded, want an error", row)
		}
	}
}

func TestAccessibilityGrantAbsentWhenNoRow(t *testing.T) {
	t.Setenv("VOX_TCC_DB_PATH", writeAccessibilityFixture(t, "", 0))

	state, err := accessibilityGrant(t.TempDir(), bundleID)
	if err != nil {
		t.Fatalf("accessibilityGrant: %v", err)
	}
	if state != grantAbsent {
		t.Errorf("state = %v, want grantAbsent", state)
	}
}

func TestAccessibilityGrantDeniedWhenAuthValueWithholds(t *testing.T) {
	t.Setenv("VOX_TCC_DB_PATH", writeAccessibilityFixture(t, bundleID, 0))

	state, err := accessibilityGrant(t.TempDir(), bundleID)
	if err != nil {
		t.Fatalf("accessibilityGrant: %v", err)
	}
	if state != grantDenied {
		t.Errorf("state = %v, want grantDenied", state)
	}
}

// A grant recorded for a different signature is the failure this check
// exists for: System Settings shows Vox as enabled while macOS ignores it.
func TestAccessibilityGrantStaleWhenRequirementUnsatisfied(t *testing.T) {
	t.Setenv("VOX_TCC_DB_PATH", writeAccessibilityFixture(t, bundleID, tccAuthValueAllowed))

	// An unsigned directory satisfies no requirement.
	state, err := accessibilityGrant(t.TempDir(), bundleID)
	if err != nil {
		t.Fatalf("accessibilityGrant: %v", err)
	}
	if state != grantStaleSignature {
		t.Errorf("state = %v, want grantStaleSignature", state)
	}
}

func TestAccessibilityGrantUnreadableWhenDatabaseMissing(t *testing.T) {
	t.Setenv("VOX_TCC_DB_PATH", filepath.Join(t.TempDir(), "absent.db"))

	state, err := accessibilityGrant(t.TempDir(), bundleID)
	if err == nil {
		t.Error("accessibilityGrant succeeded, want an error for a missing database")
	}
	if state != grantUnreadable {
		t.Errorf("state = %v, want grantUnreadable", state)
	}
}

// An empty requirement means macOS matches on the bundle ID alone, so the
// build cannot be stale.
func TestBundleSatisfiesEmptyRequirement(t *testing.T) {
	matches, err := bundleSatisfiesRequirement(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("bundleSatisfiesRequirement: %v", err)
	}
	if !matches {
		t.Error("empty requirement not satisfied, want satisfied")
	}
}

func TestAppNameOfExecutable(t *testing.T) {
	cases := []struct {
		path     string
		want     string
		inBundle bool
	}{
		{"/Applications/Ghostty.app/Contents/MacOS/ghostty", "Ghostty", true},
		{"/Users/x/code/vox/bin/Vox.app/Contents/MacOS/vox", "Vox", true},
		{"/usr/bin/login", "", false},
		{"/Users/x/code/vox/bin/vox", "", false},
	}
	for _, c := range cases {
		app, inBundle := appNameOfExecutable(c.path)
		if app != c.want || inBundle != c.inBundle {
			t.Errorf("appNameOfExecutable(%q) = (%q, %v), want (%q, %v)",
				c.path, app, inBundle, c.want, c.inBundle)
		}
	}
}

func TestAppBundlePathRejectsBareBinary(t *testing.T) {
	if _, bundled := appBundlePath(); bundled {
		t.Error("test binary reported as bundled, want bare")
	}
}

// writeAccessibilityFixture builds a TCC-shaped database holding at most one
// Accessibility row, and returns its path. A blank client writes no row.
func writeAccessibilityFixture(t *testing.T, client string, authValue int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "TCC.db")
	sql := "create table access (service text, client text, auth_value integer, csreq blob);"
	if client != "" {
		sql += fmt.Sprintf(
			"insert into access values ('kTCCServiceAccessibility', %s, %d, x'FADE0C00');",
			sqliteQuote(client), authValue,
		)
	}
	cmd := exec.Command("sqlite3", path)
	cmd.Stdin = strings.NewReader(sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture db: %v: %s", err, out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture db not created: %v", err)
	}
	return path
}
