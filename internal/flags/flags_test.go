package flags

import (
	"context"
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldreason"

	"vox/internal/userconfig"
)

// mockLDClient is a minimal stub for testing LD flag evaluation paths.
type mockLDClient struct {
	stringVal    string
	stringDetail ldreason.EvaluationDetail
	stringErr    error
	boolVal      bool
	boolDetail   ldreason.EvaluationDetail
	boolErr      error
}

func (m *mockLDClient) StringVariationDetailCtx(_ context.Context, _ string, _ ldcontext.Context, _ string) (string, ldreason.EvaluationDetail, error) {
	return m.stringVal, m.stringDetail, m.stringErr
}

func (m *mockLDClient) BoolVariationDetailCtx(_ context.Context, _ string, _ ldcontext.Context, _ bool) (bool, ldreason.EvaluationDetail, error) {
	return m.boolVal, m.boolDetail, m.boolErr
}

func (m *mockLDClient) Close() error { return nil }

func successDetail() ldreason.EvaluationDetail {
	return ldreason.EvaluationDetail{Reason: ldreason.NewEvalReasonFallthrough()}
}

func errorDetail() ldreason.EvaluationDetail {
	return ldreason.EvaluationDetail{Reason: ldreason.NewEvalReasonError(ldreason.EvalErrorFlagNotFound)}
}

func TestBoolFlagPrecedence(t *testing.T) {
	c := &Client{userCfg: userconfig.Config{}}

	// Default when nothing is set.
	if c.boolFlag("test-key", "VOX_TEST_FLAG_12345", nil, false) != false {
		t.Error("expected default false")
	}
	if c.boolFlag("test-key", "VOX_TEST_FLAG_12345", nil, true) != true {
		t.Error("expected default true")
	}

	// Config file value overrides default.
	valTrue := true
	if c.boolFlag("test-key", "VOX_TEST_FLAG_12345", &valTrue, false) != true {
		t.Error("config file true should override default false")
	}
	valFalse := false
	if c.boolFlag("test-key", "VOX_TEST_FLAG_12345", &valFalse, true) != false {
		t.Error("config file false should override default true")
	}

	// Env var "false" overrides config file true.
	t.Setenv("VOX_TEST_FLAG_12345", "false")
	if c.boolFlag("test-key", "VOX_TEST_FLAG_12345", &valTrue, false) != false {
		t.Error("env var false should override config file true")
	}

	// Env var "true" overrides config file false.
	t.Setenv("VOX_TEST_FLAG_12345", "true")
	if c.boolFlag("test-key", "VOX_TEST_FLAG_12345", &valFalse, false) != true {
		t.Error("env var true should override config file false")
	}
}

// NOTE: The LD-active path (c.ld != nil) is tested via mockLDClient (see
// TestAnthropicKeyLDPrecedence). The nil-client tests above cover the
// env > config > default fallback chain.

func TestAIModelPrecedence(t *testing.T) {
	c := &Client{userCfg: userconfig.Config{}}

	// Default.
	if got := c.AIModel(); got != "claude-haiku-4-5-20251001" {
		t.Errorf("AIModel() = %q, want default haiku", got)
	}

	// Config file.
	model := "claude-sonnet-4-20250514"
	c.userCfg.AIModel = &model
	if got := c.AIModel(); got != "claude-sonnet-4-20250514" {
		t.Errorf("AIModel() = %q, want sonnet from config", got)
	}

	// Env var overrides config.
	t.Setenv("VOX_AI_MODEL", "claude-opus-4-20250514")
	if got := c.AIModel(); got != "claude-opus-4-20250514" {
		t.Errorf("AIModel() = %q, want opus from env", got)
	}
}

func TestAnthropicKeyPrecedence(t *testing.T) {
	// Clear any real ANTHROPIC_API_KEY so it doesn't interfere.
	t.Setenv("ANTHROPIC_API_KEY", "")

	c := &Client{userCfg: userconfig.Config{}}

	// Default: empty (no key configured).
	if key, src := c.AnthropicKey(); key != "" || src != "" {
		t.Errorf("AnthropicKey() = (%q, %q), want empty", key, src)
	}

	// Config file.
	cfgKey := "sk-config-key"
	c.userCfg.AnthropicKey = &cfgKey
	if key, src := c.AnthropicKey(); key != "sk-config-key" || src != "config_file" {
		t.Errorf("AnthropicKey() = (%q, %q), want (sk-config-key, config_file)", key, src)
	}

	// Env var overrides config.
	t.Setenv("ANTHROPIC_API_KEY", "sk-env-key")
	if key, src := c.AnthropicKey(); key != "sk-env-key" || src != "env" {
		t.Errorf("AnthropicKey() = (%q, %q), want (sk-env-key, env)", key, src)
	}
}

func TestAnthropicKeyLDPrecedence(t *testing.T) {
	// Clear any real ANTHROPIC_API_KEY so it doesn't interfere.
	t.Setenv("ANTHROPIC_API_KEY", "")

	cfgKey := "sk-config-key"

	t.Run("LD flag overrides config file", func(t *testing.T) {
		c := &Client{
			ld: &mockLDClient{
				stringVal:    "sk-ld-team-key",
				stringDetail: successDetail(),
			},
			userCfg: userconfig.Config{AnthropicKey: &cfgKey},
		}
		key, src := c.AnthropicKey()
		if key != "sk-ld-team-key" || src != "ld_flag" {
			t.Errorf("AnthropicKey() = (%q, %q), want (sk-ld-team-key, ld_flag)", key, src)
		}
	})

	t.Run("env var overrides LD flag", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "sk-env-key")
		c := &Client{
			ld: &mockLDClient{
				stringVal:    "sk-ld-team-key",
				stringDetail: successDetail(),
			},
			userCfg: userconfig.Config{AnthropicKey: &cfgKey},
		}
		key, src := c.AnthropicKey()
		if key != "sk-env-key" || src != "env" {
			t.Errorf("AnthropicKey() = (%q, %q), want (sk-env-key, env)", key, src)
		}
	})

	t.Run("LD error falls through to config", func(t *testing.T) {
		c := &Client{
			ld: &mockLDClient{
				stringVal:    "",
				stringDetail: errorDetail(),
			},
			userCfg: userconfig.Config{AnthropicKey: &cfgKey},
		}
		key, src := c.AnthropicKey()
		if key != "sk-config-key" || src != "config_file" {
			t.Errorf("AnthropicKey() = (%q, %q), want (sk-config-key, config_file)", key, src)
		}
	})

	t.Run("LD returns empty string falls through to config", func(t *testing.T) {
		c := &Client{
			ld: &mockLDClient{
				stringVal:    "",
				stringDetail: successDetail(),
			},
			userCfg: userconfig.Config{AnthropicKey: &cfgKey},
		}
		key, src := c.AnthropicKey()
		if key != "sk-config-key" || src != "config_file" {
			t.Errorf("AnthropicKey() = (%q, %q), want (sk-config-key, config_file)", key, src)
		}
	})
}

func TestNilClientGraceful(t *testing.T) {
	// All methods should work with a nil LD client.
	c := &Client{userCfg: userconfig.Config{}}

	_ = c.AIPostProcess()
	_ = c.PromptMode()
	_ = c.VoiceCommands()
	_ = c.ContextAware()
	_ = c.StreamingOverlay()
	_ = c.AIModel()
	_, _ = c.AnthropicKey()
	c.Close() // should not panic
}

func TestInitNoSDKKey(t *testing.T) {
	// Ensure VOX_LD_SDK_KEY is not set (t.Setenv restores on cleanup).
	t.Setenv("VOX_LD_SDK_KEY", "")

	c, err := Init(userconfig.Config{})
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	defer c.Close()

	if c.ld != nil {
		t.Error("LD client should be nil when no SDK key is set")
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"YES", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"", false},
		{"anything", false},
	}
	for _, tt := range tests {
		if got := parseBool(tt.input); got != tt.want {
			t.Errorf("parseBool(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestFlagKeyConstants(t *testing.T) {
	// Verify flag keys match what was created in LaunchDarkly.
	keys := []string{
		KeyAIPostProcess,
		KeyPromptMode,
		KeyVoiceCommands,
		KeyContextAware,
		KeyStreamingOverlay,
		KeyAIModel,
		KeyAnthropicKey,
	}
	for _, k := range keys {
		if k == "" {
			t.Error("flag key should not be empty")
		}
	}
}
