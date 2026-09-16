package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"howett.net/plist"
)

// ledgerFixture builds a trackedApplications blob in the on-disk shape:
// a flat list where each app contributes two records — a bare
// {bundle:{_0:id}} key record followed by a {location, menuItemLocations,
// isAllowed} value record.
func ledgerFixture(t *testing.T, apps []ledgerApp) []byte {
	t.Helper()
	var entries []map[string]any
	for _, a := range apps {
		var locs []map[string]any
		for _, id := range a.menuItems {
			locs = append(locs, map[string]any{"bundle": map[string]any{"_0": id}})
		}
		entries = append(entries,
			map[string]any{"bundle": map[string]any{"_0": a.id}},
			map[string]any{
				"location":          map[string]any{"bundle": map[string]any{"_0": a.id}},
				"menuItemLocations": locs,
				"isAllowed":         a.allowed,
			},
		)
	}
	b, err := plist.Marshal(entries, plist.BinaryFormat)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return b
}

type ledgerApp struct {
	id        string
	allowed   bool
	menuItems []string
}

func TestAnalyzeLedgerCleanWhenOnlySelfReferences(t *testing.T) {
	blob := ledgerFixture(t, []ledgerApp{
		{id: "dev.vox.menubar", allowed: true, menuItems: []string{"dev.vox.menubar"}},
		{id: "com.example.other", allowed: false, menuItems: []string{"com.example.other"}},
	})

	rep, err := analyzeStatusItemLedger(blob, "dev.vox.menubar")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !rep.ownFound || rep.ownAllowed != allowYes {
		t.Errorf("own record: found=%v allowed=%v, want found+allowed", rep.ownFound, rep.ownAllowed)
	}
	if len(rep.foreignOwners) != 0 {
		t.Errorf("expected no foreign owners, got %+v", rep.foreignOwners)
	}
	if rep.blocked() {
		t.Error("clean ledger reported as blocked")
	}
}

func TestAnalyzeLedgerDetectsDisallowedForeignOwner(t *testing.T) {
	// A parent process (terminal, IDE, agent host) that is switched OFF in
	// System Settings > Menu Bar has captured Vox's bundle ID in its own
	// menuItemLocations. Vox's own record is allowed, so the Settings toggle
	// looks fine — but Control Center honours the parent's veto.
	blob := ledgerFixture(t, []ledgerApp{
		{id: "dev.vox.menubar", allowed: true, menuItems: []string{"dev.vox.menubar"}},
		{id: "com.example.parent", allowed: false, menuItems: []string{"com.example.parent", "dev.vox.menubar"}},
	})

	rep, err := analyzeStatusItemLedger(blob, "dev.vox.menubar")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !rep.ownFound || rep.ownAllowed != allowYes {
		t.Errorf("own record: found=%v allowed=%v, want found+allowed", rep.ownFound, rep.ownAllowed)
	}
	if len(rep.foreignOwners) != 1 {
		t.Fatalf("want 1 foreign owner, got %d: %+v", len(rep.foreignOwners), rep.foreignOwners)
	}
	fo := rep.foreignOwners[0]
	if fo.id != "com.example.parent" || fo.allowed != allowNo {
		t.Errorf("foreign owner = %+v, want com.example.parent disallowed", fo)
	}
	if !rep.blocked() {
		t.Error("disallowed foreign owner should report blocked")
	}
}

func TestAnalyzeLedgerAllowedForeignOwnerDoesNotBlock(t *testing.T) {
	// A parent that is ON in Settings also holds a reference. That is
	// harmless — only disallowed owners veto.
	blob := ledgerFixture(t, []ledgerApp{
		{id: "dev.vox.menubar", allowed: true, menuItems: []string{"dev.vox.menubar"}},
		{id: "com.example.terminal", allowed: true, menuItems: []string{"dev.vox.menubar"}},
	})

	rep, err := analyzeStatusItemLedger(blob, "dev.vox.menubar")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(rep.foreignOwners) != 1 || rep.foreignOwners[0].allowed != allowYes {
		t.Fatalf("want 1 allowed foreign owner, got %+v", rep.foreignOwners)
	}
	if rep.blocked() {
		t.Error("allowed foreign owner must not report blocked")
	}
}

func TestAnalyzeLedgerOwnRecordDisallowed(t *testing.T) {
	blob := ledgerFixture(t, []ledgerApp{
		{id: "dev.vox.menubar", allowed: false, menuItems: []string{"dev.vox.menubar"}},
	})

	rep, err := analyzeStatusItemLedger(blob, "dev.vox.menubar")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !rep.ownFound || rep.ownAllowed != allowNo {
		t.Errorf("own record: found=%v allowed=%v, want found+disallowed", rep.ownFound, rep.ownAllowed)
	}
	if !rep.blocked() {
		t.Error("own record disallowed should report blocked")
	}
}

func TestAnalyzeLedgerMissingOwnRecord(t *testing.T) {
	blob := ledgerFixture(t, []ledgerApp{
		{id: "com.example.other", allowed: true, menuItems: []string{"com.example.other"}},
	})

	rep, err := analyzeStatusItemLedger(blob, "dev.vox.menubar")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if rep.ownFound {
		t.Error("own record should be absent")
	}
	if rep.blocked() {
		t.Error("absent record is 'never launched', not blocked")
	}
}

func TestAnalyzeLedgerRejectsGarbage(t *testing.T) {
	if _, err := analyzeStatusItemLedger([]byte("not a plist"), "dev.vox.menubar"); err == nil {
		t.Error("expected error on invalid plist")
	}
}

// writeLedgerFixture wraps a trackedApplications blob in the outer plist
// shape Control Center writes to disk and returns the temp file path.
func writeLedgerFixture(t *testing.T, apps []ledgerApp) string {
	t.Helper()
	outer := map[string]any{"trackedApplications": ledgerFixture(t, apps)}
	b, err := plist.Marshal(outer, plist.BinaryFormat)
	if err != nil {
		t.Fatalf("marshal outer: %v", err)
	}
	p := filepath.Join(t.TempDir(), "group.com.apple.controlcenter.plist")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func runDoctorWithLedger(t *testing.T, ledger string) (string, int) {
	t.Helper()
	cmd := exec.Command("go", "run", ".", "doctor")
	cmd.Env = append(os.Environ(), "VOX_LEDGER_PATH="+ledger)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run doctor: %v\n%s", err, out)
	}
	return string(out), code
}

// TestDoctorReportsBlockedForeignOwner is the negative case: a disallowed
// parent app holds Vox's ID, so doctor must say BLOCKED, name the owner, and
// exit non-zero.
func TestDoctorReportsBlockedForeignOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping doctor subcommand test in short mode")
	}
	ledger := writeLedgerFixture(t, []ledgerApp{
		{id: bundleID, allowed: true, menuItems: []string{bundleID}},
		{id: "com.example.parent", allowed: false, menuItems: []string{"com.example.parent", bundleID}},
	})
	out, code := runDoctorWithLedger(t, ledger)

	for _, want := range []string{
		"Verdict:      BLOCKED",
		"Foreign owner: com.example.parent — DISALLOWED",
		"make doctor-fix",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for a blocked ledger", code)
	}
}

// TestDoctorReportsOKForCleanLedger is the positive case.
func TestDoctorReportsOKForCleanLedger(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping doctor subcommand test in short mode")
	}
	ledger := writeLedgerFixture(t, []ledgerApp{
		{id: bundleID, allowed: true, menuItems: []string{bundleID}},
		{id: "com.example.terminal", allowed: true, menuItems: []string{bundleID}},
	})
	out, _ := runDoctorWithLedger(t, ledger)

	if !strings.Contains(out, "Verdict:      OK") {
		t.Errorf("output missing OK verdict\n%s", out)
	}
	if !strings.Contains(out, "Foreign owner: com.example.terminal — allowed (harmless)") {
		t.Errorf("allowed foreign owner should be listed as harmless\n%s", out)
	}
	if strings.Contains(out, "BLOCKED") {
		t.Errorf("clean ledger must not say BLOCKED\n%s", out)
	}
	// Exit code is not asserted here: on a dev machine the pidfile or
	// Accessibility check may legitimately fail and drive it non-zero.
}

// TestDoctorUnreadableLedgerIsNotAPass: when the ledger can't be read the
// menu bar check is inconclusive. The summary must say so rather than
// reporting a clean bill of health.
func TestDoctorUnreadableLedgerIsNotAPass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping doctor subcommand test in short mode")
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist.plist")
	out, _ := runDoctorWithLedger(t, missing)

	if strings.Contains(out, "All checks passed.") {
		t.Errorf("unreadable ledger must not produce 'All checks passed.'\n%s", out)
	}
	if !strings.Contains(out, "could not be completed") {
		t.Errorf("summary should say a check could not be completed\n%s", out)
	}
}

// --- Review follow-ups: tri-state isAllowed ---

// isAllowedFixture is like ledgerFixture but lets a test omit or corrupt
// isAllowed on one record.
func isAllowedFixture(t *testing.T, owner string, isAllowed any, menuItems []string) []byte {
	t.Helper()
	var locs []map[string]any
	for _, id := range menuItems {
		locs = append(locs, map[string]any{"bundle": map[string]any{"_0": id}})
	}
	rec := map[string]any{
		"location":          map[string]any{"bundle": map[string]any{"_0": owner}},
		"menuItemLocations": locs,
	}
	if isAllowed != nil {
		rec["isAllowed"] = isAllowed
	}
	b, err := plist.Marshal([]map[string]any{rec}, plist.BinaryFormat)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return b
}

func TestAnalyzeLedgerMissingIsAllowedOnForeignOwnerIsUnknown(t *testing.T) {
	blob := isAllowedFixture(t, "com.example.parent", nil, []string{"com.example.parent", bundleID})
	rep, err := analyzeStatusItemLedger(blob, bundleID)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(rep.foreignOwners) != 1 {
		t.Fatalf("want 1 foreign owner, got %+v", rep.foreignOwners)
	}
	if rep.foreignOwners[0].allowed != allowUnknown {
		t.Errorf("missing isAllowed should be allowUnknown, got %v", rep.foreignOwners[0].allowed)
	}
	if rep.blocked() {
		t.Error("unknown must not be treated as a veto")
	}
	if !rep.inconclusive() {
		t.Error("unknown must make the report inconclusive")
	}
}

func TestAnalyzeLedgerNonBoolIsAllowedIsUnknown(t *testing.T) {
	blob := isAllowedFixture(t, "com.example.parent", "yes", []string{"com.example.parent", bundleID})
	rep, err := analyzeStatusItemLedger(blob, bundleID)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if rep.foreignOwners[0].allowed != allowUnknown {
		t.Errorf("non-bool isAllowed should be allowUnknown, got %v", rep.foreignOwners[0].allowed)
	}
}

func TestAnalyzeLedgerMissingIsAllowedOnOwnRecordIsUnknown(t *testing.T) {
	blob := isAllowedFixture(t, bundleID, nil, []string{bundleID})
	rep, err := analyzeStatusItemLedger(blob, bundleID)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !rep.ownFound || rep.ownAllowed != allowUnknown {
		t.Errorf("own record: found=%v allowed=%v, want found+unknown", rep.ownFound, rep.ownAllowed)
	}
	if rep.blocked() {
		t.Error("unknown own record must not report blocked")
	}
	if !rep.inconclusive() {
		t.Error("unknown own record must be inconclusive")
	}
}

// --- Review follow-ups: trackedApplications encoding ---

func TestDecodeTrackedApplicationsAcceptsEmbeddedBytes(t *testing.T) {
	blob := ledgerFixture(t, []ledgerApp{{id: bundleID, allowed: true, menuItems: []string{bundleID}}})
	entries, err := decodeTrackedApplications(blob)
	if err != nil {
		t.Fatalf("decode bytes: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(entries))
	}
}

func TestDecodeTrackedApplicationsAcceptsDecodedArray(t *testing.T) {
	arr := []any{
		map[string]any{"bundle": map[string]any{"_0": bundleID}},
		map[string]any{
			"location":          map[string]any{"bundle": map[string]any{"_0": bundleID}},
			"menuItemLocations": []any{},
			"isAllowed":         true,
		},
	}
	entries, err := decodeTrackedApplications(arr)
	if err != nil {
		t.Fatalf("decode array: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(entries))
	}
}

func TestDecodeTrackedApplicationsRejectsUnexpectedType(t *testing.T) {
	if _, err := decodeTrackedApplications("a string"); err == nil {
		t.Error("string should be rejected")
	}
	if _, err := decodeTrackedApplications(map[string]any{"x": 1}); err == nil {
		t.Error("dict should be rejected")
	}
}

// TestDoctorUnexpectedTrackedApplicationsTypeIsInconclusive: a ledger whose
// trackedApplications is neither bytes nor an array must not pass.
func TestDoctorUnexpectedTrackedApplicationsTypeIsInconclusive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping doctor subcommand test in short mode")
	}
	outer := map[string]any{"trackedApplications": "not-a-ledger"}
	b, err := plist.Marshal(outer, plist.BinaryFormat)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p := filepath.Join(t.TempDir(), "weird.plist")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, _ := runDoctorWithLedger(t, p)
	if strings.Contains(out, "All checks passed.") {
		t.Errorf("unexpected type must not pass\n%s", out)
	}
	if !strings.Contains(out, "INCONCLUSIVE") {
		t.Errorf("unexpected type should be INCONCLUSIVE\n%s", out)
	}
}

// --- Review follow-ups: PID file path ---

func TestPIDFilePathHonorsEnv(t *testing.T) {
	t.Setenv("VOX_PID_PATH", "/tmp/custom.pid")
	if got := pidFilePath(); got != "/tmp/custom.pid" {
		t.Errorf("pidFilePath() = %q, want /tmp/custom.pid", got)
	}
}

func TestPIDFilePathDefault(t *testing.T) {
	t.Setenv("VOX_PID_PATH", "")
	if got := pidFilePath(); got != filepath.Join("logs", "vox.pid") {
		t.Errorf("pidFilePath() = %q, want logs/vox.pid", got)
	}
}

func TestReadPIDFileDistinguishesAbsentFromMalformed(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "nope.pid")
	if _, err := readPIDFile(absent); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("absent file should return ErrNotExist, got %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.pid")
	if err := os.WriteFile(bad, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPIDFile(bad); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("malformed file should return a non-ErrNotExist error, got %v", err)
	}
}

// --- Review follow-ups: pgrep false positives ---

func TestIsVoxExecutableAcceptsRealPath(t *testing.T) {
	for _, exe := range []string{
		"/Users/x/code/vox/bin/Vox.app/Contents/MacOS/vox",
		"bin/Vox.app/Contents/MacOS/vox",
	} {
		if !isVoxExecutable(exe) {
			t.Errorf("%q should be recognised as Vox", exe)
		}
	}
}

func TestIsVoxExecutableRejectsMentions(t *testing.T) {
	for _, exe := range []string{
		"/usr/bin/tail", // tail -f .../Vox.app/Contents/MacOS/vox
		"/usr/bin/vim",  // vim .../Vox.app/Contents/MacOS/vox
		"/bin/zsh",      // shell whose argv mentions the path
		"/usr/bin/python3",
	} {
		if isVoxExecutable(exe) {
			t.Errorf("%q must not be counted as Vox", exe)
		}
	}
}

// TestDoctorOwnRecordDisallowedGivesToggleAdvice: when only Vox's own record
// is off (no foreign veto), the fix is the normal Settings toggle — doctor
// must not send the user to doctor-fix.
func TestDoctorOwnRecordDisallowedGivesToggleAdvice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping doctor subcommand test in short mode")
	}
	ledger := writeLedgerFixture(t, []ledgerApp{
		{id: bundleID, allowed: false, menuItems: []string{bundleID}},
	})
	out, code := runDoctorWithLedger(t, ledger)

	if !strings.Contains(out, "Verdict:      BLOCKED") {
		t.Errorf("own record off should be BLOCKED\n%s", out)
	}
	if !strings.Contains(out, "Toggle it on there") {
		t.Errorf("should advise the Settings toggle\n%s", out)
	}
	if strings.Contains(out, "make doctor-fix") {
		t.Errorf("must not recommend doctor-fix when there is no foreign veto\n%s", out)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
