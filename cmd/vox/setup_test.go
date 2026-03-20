package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestSetupSubcommand verifies that "vox setup" runs without error and prints
// the expected permission-check output. Requires the binary to be built first.
func TestSetupSubcommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping setup subcommand test in short mode")
	}

	out, err := exec.Command("go", "run", ".", "setup").CombinedOutput()
	if err != nil {
		t.Fatalf("vox setup exited with error: %v\noutput: %s", err, out)
	}

	got := string(out)
	for _, want := range []string{"Accessibility:", "Microphone:", "Done."} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}
