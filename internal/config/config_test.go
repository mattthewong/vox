package config

import (
	"path/filepath"
	"strings"
	"testing"

	"vox/internal/hotkey"
	"vox/internal/sttmodel"
)

// isolateModelEnv makes model-ID resolution independent of the developer's
// machine. Without it these tests read the real prefs.json and inherit any
// VOX_STT_ENGINE / VOX_WHISPER_MODEL_ID already exported in the shell, so they
// pass on clean CI and fail on a working dev box. Both model roots are
// redirected too, so nothing here can observe a locally installed model.
//
// Individual tests override the specific variable under test after calling it.
func isolateModelEnv(t *testing.T) {
	t.Helper()
	t.Setenv("VOX_MODEL_DIR", t.TempDir())
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	t.Setenv("VOX_PREFS_PATH", filepath.Join(t.TempDir(), "prefs.json"))
	t.Setenv("VOX_STT_ENGINE", "")
	t.Setenv("VOX_WHISPER_MODEL_ID", "")
}

func TestLoadDefaults(t *testing.T) {
	isolateModelEnv(t)
	t.Setenv("VOX_LANGUAGE", "")
	t.Setenv("VOX_HOLD_TO_TALK", "")
	t.Setenv("VOX_VERBOSE", "")
	t.Setenv("VOX_HOTKEY", "")

	cfg := Load()

	if cfg.Language != "" {
		t.Errorf("Language = %q, want %q", cfg.Language, "")
	}
	if !cfg.HoldToTalk {
		t.Error("HoldToTalk = false, want true")
	}
	if cfg.Verbose {
		t.Error("Verbose = true, want false")
	}
	if cfg.Hotkey != "option+space" {
		t.Errorf("Hotkey = %q, want %q", cfg.Hotkey, "option+space")
	}
	if cfg.ModelID != "base.en" {
		t.Errorf("ModelID = %q, want %q", cfg.ModelID, "base.en")
	}
}

func TestLoadFromEnv(t *testing.T) {
	isolateModelEnv(t)
	t.Setenv("VOX_LANGUAGE", "en")
	t.Setenv("VOX_HOLD_TO_TALK", "false")
	t.Setenv("VOX_VERBOSE", "true")
	t.Setenv("VOX_HOTKEY", "cmd+shift")
	t.Setenv("VOX_WHISPER_MODEL_ID", "small.en")

	cfg := Load()

	if cfg.Language != "en" {
		t.Errorf("Language = %q, want %q", cfg.Language, "en")
	}
	if cfg.HoldToTalk {
		t.Error("HoldToTalk = true, want false")
	}
	if !cfg.Verbose {
		t.Error("Verbose = false, want true")
	}
	if cfg.ModelID != "small.en" {
		t.Errorf("ModelID = %q, want %q", cfg.ModelID, "small.en")
	}
}

func TestModelIDDefault(t *testing.T) {
	isolateModelEnv(t)

	cfg := Load()
	if cfg.ModelID != sttmodel.DefaultID {
		t.Errorf("ModelID = %q, want %q", cfg.ModelID, sttmodel.DefaultID)
	}
}

func TestModelIDAcceptsParakeet(t *testing.T) {
	isolateModelEnv(t)
	t.Setenv("VOX_STT_ENGINE", "parakeet-v2")

	cfg := Load()
	if cfg.ModelID != "parakeet-v2" {
		t.Errorf("ModelID = %q, want parakeet-v2", cfg.ModelID)
	}
}

func TestModelIDAcceptsBareEngineName(t *testing.T) {
	// VOX_STT_ENGINE carries either a model ID or a bare engine name.
	// config.Load passes the raw value through; resolveEngineModel in
	// cmd/vox interprets it. Load must not reject "parakeet" just because
	// it is not a catalog ID.
	isolateModelEnv(t)
	t.Setenv("VOX_STT_ENGINE", "parakeet")

	cfg := Load()
	if cfg.ModelID != "parakeet" {
		t.Errorf("ModelID = %q, want parakeet (passed through verbatim)", cfg.ModelID)
	}
}

func TestModelIDLegacyEnvStillWorks(t *testing.T) {
	isolateModelEnv(t)
	t.Setenv("VOX_WHISPER_MODEL_ID", "small.en")

	cfg := Load()
	if cfg.ModelID != "small.en" {
		t.Errorf("ModelID = %q, want small.en", cfg.ModelID)
	}
}

func TestModelIDNewEnvBeatsLegacy(t *testing.T) {
	isolateModelEnv(t)
	t.Setenv("VOX_STT_ENGINE", "parakeet-v2")
	t.Setenv("VOX_WHISPER_MODEL_ID", "small.en")

	cfg := Load()
	if cfg.ModelID != "parakeet-v2" {
		t.Errorf("ModelID = %q, want parakeet-v2", cfg.ModelID)
	}
}

func TestModelIDFallsBackToPref(t *testing.T) {
	isolateModelEnv(t)

	if err := SavePref(func(p *Prefs) { p.Model = "small.en" }); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if cfg.ModelID != "small.en" {
		t.Errorf("ModelID = %q, want small.en from prefs", cfg.ModelID)
	}
}

func TestBoolParsing(t *testing.T) {
	tests := []struct {
		input      string
		defaultVal bool
		want       bool
	}{
		{"true", false, true}, {"True", false, true}, {"TRUE", false, true},
		{"1", false, true}, {"yes", false, true}, {"Yes", false, true},
		{"false", true, false}, {"0", true, false}, {"no", true, false},
		{"anything", true, false},
		{"", true, true}, {"", false, false},
	}

	for _, tt := range tests {
		got := parseBool(tt.input, tt.defaultVal)
		if got != tt.want {
			t.Errorf("parseBool(%q, %t) = %t, want %t", tt.input, tt.defaultVal, got, tt.want)
		}
	}
}

func TestConfigString(t *testing.T) {
	cfg := Config{
		Language:   "en",
		HoldToTalk: true,
		ModelID:    "base.en",
		Verbose:    false,
		Triggers:   []hotkey.Trigger{{Label: "Option+Space"}},
	}
	s := cfg.String()
	for _, substr := range []string{"en", "hold-to-talk", "Option+Space", "base.en"} {
		if !strings.Contains(s, substr) {
			t.Errorf("String() = %q, missing %q", s, substr)
		}
	}
}

func TestParseHotkeys(t *testing.T) {
	tests := []struct {
		input   string
		count   int
		wantErr bool
	}{
		{"fn", 1, false},
		{"cmd+shift", 1, false},
		{"option+space", 1, false},
		{"fn,cmd+shift", 2, false},
		{"ctrl+shift+d", 1, false},
		{"CMD+SHIFT", 1, false},
		{"", 0, true},
		{"badmod+space", 0, true},
		{"option+invalid", 0, true},
	}

	for _, tt := range tests {
		triggers, err := ParseHotkeys(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseHotkeys(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseHotkeys(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if len(triggers) != tt.count {
			t.Errorf("ParseHotkeys(%q) returned %d triggers, want %d", tt.input, len(triggers), tt.count)
		}
	}
}

func TestParseFn(t *testing.T) {
	triggers, err := ParseHotkeys("fn")
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 1 || !triggers[0].Fn {
		t.Errorf("expected Fn trigger, got %+v", triggers)
	}
	if triggers[0].Key != -1 {
		t.Errorf("Fn trigger Key = %d, want -1", triggers[0].Key)
	}
}

func TestParseModifierOnly(t *testing.T) {
	triggers, err := ParseHotkeys("cmd+shift")
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	tr := triggers[0]
	if tr.Key != -1 {
		t.Errorf("modifier-only trigger Key = %d, want -1", tr.Key)
	}
	want := hotkey.FlagCommand | hotkey.FlagShift
	if tr.Modifiers != want {
		t.Errorf("Modifiers = 0x%x, want 0x%x", tr.Modifiers, want)
	}
}

func TestParseModifierPlusKey(t *testing.T) {
	triggers, err := ParseHotkeys("option+space")
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	tr := triggers[0]
	if tr.Key != 49 { // space keycode
		t.Errorf("Key = %d, want 49 (space)", tr.Key)
	}
	if tr.Modifiers != hotkey.FlagOption {
		t.Errorf("Modifiers = 0x%x, want 0x%x", tr.Modifiers, hotkey.FlagOption)
	}
}

func TestParseMultipleHotkeys(t *testing.T) {
	triggers, err := ParseHotkeys("fn,cmd+shift")
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(triggers))
	}
	if !triggers[0].Fn {
		t.Error("first trigger should be Fn")
	}
	if triggers[1].Key != -1 || triggers[1].Modifiers != hotkey.FlagCommand|hotkey.FlagShift {
		t.Errorf("second trigger = %+v, want cmd+shift modifier-only", triggers[1])
	}
}

func TestTriggerLabels(t *testing.T) {
	tests := []struct {
		input string
		label string
	}{
		{"fn", "Fn"},
		{"cmd+shift", "Cmd+Shift"},
		{"option+space", "Option+Space"},
		{"ctrl+shift+d", "Ctrl+Shift+D"},
	}
	for _, tt := range tests {
		triggers, err := ParseHotkeys(tt.input)
		if err != nil {
			t.Fatalf("ParseHotkeys(%q): %v", tt.input, err)
		}
		if triggers[0].Label != tt.label {
			t.Errorf("ParseHotkeys(%q) label = %q, want %q", tt.input, triggers[0].Label, tt.label)
		}
	}
}
