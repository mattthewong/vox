//go:build darwin

package inject

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestTypeTextEmpty(t *testing.T) {
	// Empty string must be a no-op — no clipboard interaction, no error.
	if err := TypeText(""); err != nil {
		t.Fatalf("TypeText(\"\") returned unexpected error: %v", err)
	}
}

func TestCopyToClipboard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping clipboard test in short mode")
	}

	// Use a unique string to avoid false positives from stale clipboard content.
	want := fmt.Sprintf("vox-test-clipboard-%d", time.Now().UnixNano())

	if err := copyToClipboard(want); err != nil {
		t.Fatalf("copyToClipboard(%q) error: %v", want, err)
	}

	got, err := readClipboard()
	if err != nil {
		t.Fatalf("readClipboard error: %v", err)
	}

	if got != want {
		t.Errorf("clipboard content mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// readClipboard returns the current system clipboard text via pbpaste.
func readClipboard() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return "", fmt.Errorf("pbpaste: %w", err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}
