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
	t.Setenv("VOX_TCC_DB_PATH", writeAccessibilityFixture(t, "", 0, nil))

	state, err := recordedAccessibilityGrant(t.TempDir(), bundleID)
	if err != nil {
		t.Fatalf("recordedAccessibilityGrant: %v", err)
	}
	if state != grantAbsent {
		t.Errorf("state = %v, want grantAbsent", state)
	}
}

func TestAccessibilityGrantDeniedWhenAuthValueWithholds(t *testing.T) {
	t.Setenv("VOX_TCC_DB_PATH", writeAccessibilityFixture(t, bundleID, 0, nil))

	state, err := recordedAccessibilityGrant(t.TempDir(), bundleID)
	if err != nil {
		t.Fatalf("recordedAccessibilityGrant: %v", err)
	}
	if state != grantDenied {
		t.Errorf("state = %v, want grantDenied", state)
	}
}

// A grant recorded for a different signature is the failure this check
// exists for: System Settings shows Vox as enabled while macOS ignores it.
//
// The bundle here carries the right identifier and a valid ad-hoc signature,
// so the only clause it fails is the certificate the grant is anchored to —
// exactly the shape of a grant left behind by a differently signed build.
func TestAccessibilityGrantStaleWhenSignedByAnotherIdentity(t *testing.T) {
	requirement := compileRequirement(t, `identifier "`+bundleID+`" and `+
		`certificate leaf = H"0000000000000000000000000000000000000000"`)
	t.Setenv("VOX_TCC_DB_PATH",
		writeAccessibilityFixture(t, bundleID, tccAuthValueAllowed, requirement))

	state, err := recordedAccessibilityGrant(adHocSignedBundle(t), bundleID)
	if err != nil {
		t.Fatalf("recordedAccessibilityGrant: %v", err)
	}
	if state != grantStaleSignature {
		t.Errorf("state = %v, want grantStaleSignature", state)
	}
}

// The healthy case: the bundle satisfies the requirement the grant is bound
// to, so macOS will honour it.
func TestAccessibilityGrantActiveWhenBundleSatisfiesRequirement(t *testing.T) {
	requirement := compileRequirement(t, `identifier "`+bundleID+`"`)
	t.Setenv("VOX_TCC_DB_PATH",
		writeAccessibilityFixture(t, bundleID, tccAuthValueAllowed, requirement))

	state, err := recordedAccessibilityGrant(adHocSignedBundle(t), bundleID)
	if err != nil {
		t.Fatalf("recordedAccessibilityGrant: %v", err)
	}
	if state != grantActive {
		t.Errorf("state = %v, want grantActive", state)
	}
}

func TestAccessibilityGrantUnreadableWhenDatabaseMissing(t *testing.T) {
	t.Setenv("VOX_TCC_DB_PATH", filepath.Join(t.TempDir(), "absent.db"))

	state, err := recordedAccessibilityGrant(t.TempDir(), bundleID)
	if err == nil {
		t.Error("recordedAccessibilityGrant succeeded, want an error for a missing database")
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
func writeAccessibilityFixture(t *testing.T, client string, authValue int, requirement []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "TCC.db")
	sql := "create table access (service text, client text, auth_value integer, csreq blob);"
	if client != "" {
		sql += fmt.Sprintf(
			"insert into access values ('kTCCServiceAccessibility', %s, %d, x'%s');",
			sqliteQuote(client), authValue, hex.EncodeToString(requirement),
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

// compileRequirement turns requirement source into the compiled blob macOS
// stores in the csreq column, so a fixture holds the same bytes a real grant
// does. A malformed blob makes codesign fail for its own reasons, which would
// let a requirement check pass without ever being evaluated.
func compileRequirement(t *testing.T, text string) []byte {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "requirement.txt")
	if err := os.WriteFile(src, []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "requirement.bin")
	if got, err := exec.Command("csreq", "-r", src, "-b", out).CombinedOutput(); err != nil {
		t.Fatalf("csreq %q: %v: %s", text, err, got)
	}
	blob, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// adHocSignedBundle builds the smallest .app codesign will accept, signed
// ad-hoc under Vox's bundle ID, and returns its path. Ad-hoc signing is what
// leaves a grant stale: the bundle is valid and correctly identified, it just
// has no certificate for a requirement to anchor to.
func adHocSignedBundle(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "Vox.app")
	macOS := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		t.Fatal(err)
	}
	// Any Mach-O will do; the signature is what is under test, not the code.
	binary, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macOS, "vox"), binary, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>vox</string>
<key>CFBundleIdentifier</key><string>` + bundleID + `</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("codesign", "--force", "--sign", "-", "--identifier", bundleID, app)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, out)
	}
	return app
}
