package config

import (
	"fmt"
	"os"
	"strings"

	"vox/internal/hotkey"
)

// Config holds runtime configuration for the vox dictation tool.
type Config struct {
	WhisperURL string
	Language   string
	HoldToTalk bool
	Verbose    bool
	Hotkey     string
	Triggers   []hotkey.Trigger
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	hotkeyStr := envOrDefault("VOX_HOTKEY", "option+space")
	triggers, err := ParseHotkeys(hotkeyStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid VOX_HOTKEY=%q: %v\n", hotkeyStr, err)
		fmt.Fprintln(os.Stderr, "Examples: fn, cmd+shift, option+space, fn,cmd+shift")
		os.Exit(1)
	}

	return Config{
		WhisperURL: envOrDefault("WHISPER_URL", "http://127.0.0.1:2022"),
		Language:   os.Getenv("VOX_LANGUAGE"),
		HoldToTalk: parseBool(os.Getenv("VOX_HOLD_TO_TALK"), true),
		Verbose:    parseBool(os.Getenv("VOX_VERBOSE"), false),
		Hotkey:     hotkeyStr,
		Triggers:   triggers,
	}
}

// String returns a human-readable representation of the config.
func (c Config) String() string {
	lang := c.Language
	if lang == "" {
		lang = "auto-detect"
	}

	mode := "hold-to-talk"
	if !c.HoldToTalk {
		mode = "toggle"
	}

	var labels []string
	for _, t := range c.Triggers {
		labels = append(labels, t.Label)
	}
	hk := strings.Join(labels, " or ")
	if hk == "" {
		hk = c.Hotkey
	}

	return fmt.Sprintf(
		"WhisperURL=%s Language=%s Mode=%s Hotkey=%s Verbose=%t",
		c.WhisperURL, lang, mode, hk, c.Verbose,
	)
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func parseBool(raw string, defaultVal bool) bool {
	if raw == "" {
		return defaultVal
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// modifierMap maps lowercase names to CGEvent modifier flags.
var modifierMap = map[string]uint64{
	"ctrl":    hotkey.FlagControl,
	"control": hotkey.FlagControl,
	"shift":   hotkey.FlagShift,
	"option":  hotkey.FlagOption,
	"alt":     hotkey.FlagOption,
	"cmd":     hotkey.FlagCommand,
	"command": hotkey.FlagCommand,
	"super":   hotkey.FlagCommand,
}

// keyMap maps lowercase key names to macOS virtual keycodes.
var keyMap = map[string]int{
	"space": 49, "return": 0x24, "enter": 0x24,
	"escape": 0x35, "esc": 0x35, "delete": 0x33, "tab": 0x30,
	"left": 0x7B, "right": 0x7C, "up": 0x7E, "down": 0x7D,
	"a": 0, "b": 11, "c": 8, "d": 2, "e": 14, "f": 3, "g": 5, "h": 4,
	"i": 34, "j": 38, "k": 40, "l": 37, "m": 46, "n": 45, "o": 31, "p": 35,
	"q": 12, "r": 15, "s": 1, "t": 17, "u": 32, "v": 9, "w": 13, "x": 7,
	"y": 16, "z": 6,
	"0": 29, "1": 18, "2": 19, "3": 20, "4": 21, "5": 23, "6": 22,
	"7": 26, "8": 28, "9": 25,
	"f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76, "f5": 0x60,
	"f6": 0x61, "f7": 0x62, "f8": 0x64, "f9": 0x65, "f10": 0x6D,
	"f11": 0x67, "f12": 0x6F, "f13": 0x69, "f14": 0x6B, "f15": 0x71,
	"f16": 0x6A, "f17": 0x40, "f18": 0x4F, "f19": 0x50, "f20": 0x5A,
}

// modifierDisplayNames maps flags to display names.
var modifierDisplayNames = map[uint64]string{
	hotkey.FlagControl: "Ctrl",
	hotkey.FlagShift:   "Shift",
	hotkey.FlagOption:  "Option",
	hotkey.FlagCommand: "Cmd",
}

// ParseHotkeys parses a comma-separated list of hotkey definitions.
// Supports: "fn", "cmd+shift" (modifier-only), "option+space" (modifier+key).
func ParseHotkeys(s string) ([]hotkey.Trigger, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty hotkey string")
	}

	parts := strings.Split(s, ",")
	var triggers []hotkey.Trigger

	for _, part := range parts {
		t, err := parseSingleHotkey(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		triggers = append(triggers, t)
	}

	return triggers, nil
}

func parseSingleHotkey(s string) (hotkey.Trigger, error) {
	if s == "" {
		return hotkey.Trigger{}, fmt.Errorf("empty hotkey")
	}

	// Special case: fn/globe key.
	if strings.ToLower(s) == "fn" {
		return hotkey.Trigger{Fn: true, Key: -1, Label: "Fn"}, nil
	}

	tokens := strings.Split(strings.ToLower(s), "+")
	if len(tokens) == 0 {
		return hotkey.Trigger{}, fmt.Errorf("empty hotkey")
	}

	// Try to resolve last token as a key.
	lastToken := strings.TrimSpace(tokens[len(tokens)-1])
	keyCode, isKey := keyMap[lastToken]

	var mods uint64
	var modLabels []string

	// If the last token is a key, modifiers are everything before it.
	// If the last token is a modifier, this is a modifier-only hotkey.
	if isKey && len(tokens) > 1 {
		// modifier+key hotkey
		for _, tok := range tokens[:len(tokens)-1] {
			tok = strings.TrimSpace(tok)
			flag, ok := modifierMap[tok]
			if !ok {
				return hotkey.Trigger{}, fmt.Errorf("unknown modifier %q (valid: ctrl, shift, option/alt, cmd/command)", tok)
			}
			mods |= flag
			modLabels = append(modLabels, modifierDisplayNames[flag])
		}
		keyLabel := strings.ToUpper(lastToken[:1]) + lastToken[1:]
		if lastToken == "space" {
			keyLabel = "Space"
		}
		label := strings.Join(append(modLabels, keyLabel), "+")
		return hotkey.Trigger{Modifiers: mods, Key: keyCode, Label: label}, nil
	}

	// All tokens are modifiers (modifier-only hotkey).
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		flag, ok := modifierMap[tok]
		if !ok {
			// Last token might be an unknown key.
			if tok == lastToken && !isKey {
				return hotkey.Trigger{}, fmt.Errorf("unknown key or modifier %q", tok)
			}
			return hotkey.Trigger{}, fmt.Errorf("unknown modifier %q", tok)
		}
		mods |= flag
		modLabels = append(modLabels, modifierDisplayNames[flag])
	}

	if mods == 0 {
		return hotkey.Trigger{}, fmt.Errorf("no modifiers specified in %q", s)
	}

	return hotkey.Trigger{Modifiers: mods, Key: -1, Label: strings.Join(modLabels, "+")}, nil
}
