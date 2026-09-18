package main

import (
	"os"
	"path/filepath"
	"strings"
)

// homebrewToolDirs is where Homebrew installs the command-line tools Vox
// shells out to — sox, ffmpeg, whisper-server, brew itself — on Apple silicon
// and Intel respectively.
var homebrewToolDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// ensureHomebrewOnPath appends the Homebrew directories to PATH when they are
// missing.
//
// Finder and launchd start Vox with PATH=/usr/bin:/bin:/usr/sbin:/sbin, so an
// installed Vox.app cannot see a Homebrew sox that the same build finds
// immediately when run from a terminal. Appending rather than prepending
// leaves a user's own PATH in charge of which copy wins.
func ensureHomebrewOnPath() {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	present := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		present[dir] = true
	}
	missing := false
	for _, dir := range homebrewToolDirs {
		if !present[dir] {
			dirs = append(dirs, dir)
			missing = true
		}
	}
	if !missing {
		return
	}
	os.Setenv("PATH", strings.Join(dirs, string(filepath.ListSeparator)))
}
