package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHelpOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary test in short mode")
	}

	out, err := exec.Command("go", "run", ".", "help").CombinedOutput()
	if err != nil {
		t.Fatalf("vox help failed: %v\n%s", err, out)
	}

	output := string(out)
	checks := []string{
		"Usage: vox",
		"Commands:",
		"setup",
		"help",
		"version",
		"VOX_HOTKEY",
		"VOX_HOLD_TO_TALK",
		"VOX_LANGUAGE",
		"VOX_VERBOSE",
		"VOX_WHISPER_MODEL_ID",
		"Quick Start",
		"make setup",
		"make start",
		"github.com/mattthewong/vox",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("help output missing %q", check)
		}
	}
}

func TestHelpFlags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary test in short mode")
	}

	for _, flag := range []string{"--help", "-h"} {
		out, err := exec.Command("go", "run", ".", flag).CombinedOutput()
		if err != nil {
			t.Fatalf("vox %s failed: %v\n%s", flag, err, out)
		}
		if !strings.Contains(string(out), "Usage: vox") {
			t.Errorf("vox %s did not show help output", flag)
		}
	}
}

func TestVersionOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary test in short mode")
	}

	out, err := exec.Command("go", "run", ".", "version").CombinedOutput()
	if err != nil {
		t.Fatalf("vox version failed: %v\n%s", err, out)
	}

	// go run may emit linker warnings before the actual output;
	// check the last non-empty line.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine != version {
		t.Errorf("version output = %q, want %q", lastLine, version)
	}
}

func TestUnknownCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary test in short mode")
	}

	out, err := exec.Command("go", "run", ".", "foobar").CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown command, got success")
	}
	output := string(out)
	if !strings.Contains(output, `unknown command "foobar"`) {
		t.Errorf("expected unknown command error, got: %s", output)
	}
}
