package userconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const configDir = ".vox"
const configFile = "config.yaml"

// Config holds user preferences for Vox features.
// These can be overridden by env vars or LD flags.
type Config struct {
	AIPostProcess    *bool   `yaml:"ai_postprocess,omitempty"`
	PromptMode       *bool   `yaml:"prompt_mode,omitempty"`
	VoiceCommands    *bool   `yaml:"voice_commands,omitempty"`
	ContextAware     *bool   `yaml:"context_aware,omitempty"`
	StreamingOverlay *bool   `yaml:"streaming_overlay,omitempty"`
	AIModel          *string `yaml:"ai_model,omitempty"`
	AnthropicKey     *string `yaml:"anthropic_key,omitempty"`
	STTEngine        *string `yaml:"stt_engine,omitempty"`
}

// Path returns the full path to the config file (~/.vox/config.yaml).
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, configDir, configFile), nil
}

// Load reads the config file. Returns an empty Config (all nil) if the
// file does not exist -- this is the normal case for new users.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config file atomically, creating ~/.vox/ if needed.
// It writes to a temporary file first, then renames — so a crash can
// never leave a truncated config.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Atomic write: temp file in the same directory, then rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp config %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Clean up temp file on rename failure.
		_ = os.Remove(tmp)
		return fmt.Errorf("rename config %s: %w", path, err)
	}
	return nil
}

// BoolPtr is a helper to create a *bool for config fields.
func BoolPtr(b bool) *bool { return &b }

// StringPtr is a helper to create a *string for config fields.
func StringPtr(s string) *string { return &s }
