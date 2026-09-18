package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The PATH a Finder launch inherits. Vox has to reach Homebrew from here.
const launchdPath = "/usr/bin:/bin:/usr/sbin:/sbin"

func TestHomebrewAddedToLaunchdPath(t *testing.T) {
	t.Setenv("PATH", launchdPath)

	ensureHomebrewOnPath()

	dirs := filepath.SplitList(os.Getenv("PATH"))
	for _, want := range homebrewToolDirs {
		if !slices.Contains(dirs, want) {
			t.Errorf("PATH is missing %s; a Finder launch would not find sox", want)
		}
	}
}

// The tools a user has chosen must keep winning over the Homebrew copies.
func TestExistingPathEntriesKeepPriority(t *testing.T) {
	t.Setenv("PATH", "/my/tools:"+launchdPath)

	ensureHomebrewOnPath()

	dirs := filepath.SplitList(os.Getenv("PATH"))
	if len(dirs) == 0 || dirs[0] != "/my/tools" {
		t.Errorf("PATH = %v, want /my/tools first", dirs)
	}
}

func TestHomebrewNotDuplicated(t *testing.T) {
	t.Setenv("PATH", "/opt/homebrew/bin:/usr/local/bin:"+launchdPath)
	before := os.Getenv("PATH")

	ensureHomebrewOnPath()

	if got := os.Getenv("PATH"); got != before {
		t.Errorf("PATH = %q, want it left alone at %q", got, before)
	}
}

func TestHomebrewAddedToEmptyPath(t *testing.T) {
	t.Setenv("PATH", "")

	ensureHomebrewOnPath()

	dirs := filepath.SplitList(os.Getenv("PATH"))
	for _, want := range homebrewToolDirs {
		if !slices.Contains(dirs, want) {
			t.Errorf("PATH = %v, want it to contain %s", dirs, want)
		}
	}
}
