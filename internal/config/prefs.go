package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Prefs holds user-mutable settings persisted across launches. This is
// distinct from Config: Config is the resolved runtime state for a single
// process; Prefs is the on-disk source of truth the user edits via the UI.
//
// Boolean fields are *bool so we can distinguish "not set in the file"
// from "explicitly set to false". Callers should resolve nil via the
// shipped defaults in config.Load.
type Prefs struct {
	Hotkey        string `json:"hotkey,omitempty"`
	HoldToTalk    *bool  `json:"hold_to_talk,omitempty"`
	SoundsEnabled *bool  `json:"sounds_enabled,omitempty"`
	AutoPaste     *bool  `json:"auto_paste,omitempty"`
	AIPostProcess *bool  `json:"ai_postprocess,omitempty"`
	PromptMode    *bool  `json:"prompt_mode,omitempty"`
	VoiceCommands *bool  `json:"voice_commands,omitempty"`
	ContextAware  *bool  `json:"context_aware,omitempty"`
	Model         string `json:"model,omitempty"`
}

// BoolPtr is a small helper for callers building Prefs literals.
func BoolPtr(b bool) *bool { return &b }

// BoolOr resolves a *bool against a default.
func BoolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// PrefsPath returns the absolute path to the preferences file, following
// Apple's "Application Support" convention. Honors the VOX_PREFS_PATH env
// var (intended for tests and power users) when set.
func PrefsPath() (string, error) {
	if p := os.Getenv("VOX_PREFS_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Vox", "preferences.json"), nil
}

// LoadPrefs reads the preferences file. A missing file is not an error —
// callers get an empty Prefs and can proceed with defaults.
func LoadPrefs() (Prefs, error) {
	path, err := PrefsPath()
	if err != nil {
		return Prefs{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Prefs{}, nil
		}
		return Prefs{}, err
	}
	var p Prefs
	if err := json.Unmarshal(b, &p); err != nil {
		return Prefs{}, err
	}
	return p, nil
}

// saveMu serializes SavePref so concurrent goroutines (e.g. the hotkey
// watcher and the settings watcher) can't interleave their read-modify-write
// cycles and clobber each other's fields. Direct callers of SavePrefs
// bypass this; SavePref is the canonical entry point for partial updates.
var saveMu sync.Mutex

// SavePref is the safe way to update a single field: it reads the existing
// prefs (so other fields are preserved), runs the mutator, and writes back.
// Missing or corrupt files start from a zero Prefs rather than failing.
// Concurrent calls are serialized via saveMu.
func SavePref(mutate func(*Prefs)) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	p, _ := LoadPrefs()
	mutate(&p)
	return SavePrefs(p)
}

// SavePrefs writes the preferences file atomically (write-to-temp + rename)
// so a crash mid-write can't leave a half-baked JSON file on disk.
func SavePrefs(p Prefs) error {
	path, err := PrefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
