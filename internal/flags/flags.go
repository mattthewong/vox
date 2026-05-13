package flags

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	ld "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/ldcomponents"

	"vox/internal/userconfig"
)

// Flag key constants -- must match the keys created in LaunchDarkly.
const (
	KeyAIPostProcess    = "vox-ai-postprocess"
	KeyPromptMode       = "vox-prompt-mode"
	KeyVoiceCommands    = "vox-voice-commands"
	KeyContextAware     = "vox-context-aware"
	KeyStreamingOverlay = "vox-streaming-overlay"
	KeyAIModel          = "vox-ai-model"
	KeyAnthropicKey     = "vox-anthropic-key"
)

// ldClient is the subset of the LD SDK we use, extracted as an interface so
// unit tests can supply a stub without a running LD server.
type ldClient interface {
	BoolVariationDetailCtx(ctx context.Context, key string, evalCtx ldcontext.Context, defaultVal bool) (bool, ldreason.EvaluationDetail, error)
	StringVariationDetailCtx(ctx context.Context, key string, evalCtx ldcontext.Context, defaultVal string) (string, ldreason.EvaluationDetail, error)
	Close() error
}

// Client wraps the LD SDK client with Vox-specific flag helpers.
// When the underlying LD client is nil (no SDK key), all evaluations
// fall back to the user config file and environment variables.
type Client struct {
	ld      ldClient
	ctx     ldcontext.Context
	userCfg userconfig.Config
}

// Init creates a Client. If VOX_LD_SDK_KEY is not set, the LD client
// is nil and all flags return values from the config file or env vars.
// This is the expected path for open-source users.
func Init(userCfg userconfig.Config) (*Client, error) {
	// USER is a best-effort identifier for LD targeting context.
	// It's trivially spoofable — fine for a developer tool where the user
	// is only affecting their own experience.
	user := os.Getenv("USER")
	if user == "" {
		user = "anonymous"
	}

	ldCtx := ldcontext.NewBuilder(user).
		Kind("user").
		SetString("tool", "vox").
		Build()

	c := &Client{
		ctx:     ldCtx,
		userCfg: userCfg,
	}

	sdkKey := os.Getenv("VOX_LD_SDK_KEY")
	if sdkKey == "" {
		return c, nil
	}

	config := ld.Config{
		// Polling is better for short-lived CLI tools -- avoids holding
		// a streaming connection open.
		DataSource: ldcomponents.PollingDataSource().PollInterval(5 * time.Minute),
		// Skip event sending for a developer tool.
		Events: ldcomponents.NoEvents(),
	}

	ldClient, err := ld.MakeCustomClient(sdkKey, config, 3*time.Second)
	if err != nil {
		// MakeCustomClient may return a usable client even on timeout.
		// Keep it — it will finish initializing in the background.
		// If it's nil on a hard failure, Close() is a safe no-op.
		c.ld = ldClient
		fmt.Fprintf(os.Stderr, "Warning: LD client init (will use defaults until ready): %v\n", err)
		return c, nil
	}
	c.ld = ldClient
	return c, nil
}

// Close shuts down the LD client if it was initialized.
func (c *Client) Close() {
	if c != nil && c.ld != nil {
		_ = c.ld.Close()
	}
}

// AIPostProcess returns whether AI post-processing is enabled.
// Precedence: LD flag > VOX_AI_POSTPROCESS env > config file > false.
func (c *Client) AIPostProcess() bool {
	return c.boolFlag(KeyAIPostProcess, "VOX_AI_POSTPROCESS", c.userCfg.AIPostProcess, false)
}

// PromptMode returns whether prompt mode is enabled.
func (c *Client) PromptMode() bool {
	return c.boolFlag(KeyPromptMode, "VOX_PROMPT_MODE", c.userCfg.PromptMode, false)
}

// VoiceCommands returns whether voice commands are enabled.
func (c *Client) VoiceCommands() bool {
	return c.boolFlag(KeyVoiceCommands, "VOX_VOICE_COMMANDS", c.userCfg.VoiceCommands, false)
}

// ContextAware returns whether context-aware formatting is enabled.
func (c *Client) ContextAware() bool {
	return c.boolFlag(KeyContextAware, "VOX_CONTEXT_AWARE", c.userCfg.ContextAware, false)
}

// StreamingOverlay returns whether the streaming overlay is enabled.
func (c *Client) StreamingOverlay() bool {
	return c.boolFlag(KeyStreamingOverlay, "VOX_STREAMING_OVERLAY", c.userCfg.StreamingOverlay, false)
}

// AIModel returns the Claude model to use.
// Precedence: LD flag > VOX_AI_MODEL env > config file > default.
func (c *Client) AIModel() string {
	const defaultModel = "claude-haiku-4-5-20251001"

	if c.ld != nil {
		val, detail, _ := c.ld.StringVariationDetailCtx(
			context.Background(), KeyAIModel, c.ctx, "",
		)
		if detail.Reason.GetKind() != ldreason.EvalReasonError && val != "" {
			return val
		}
		// Flag not found, error, or empty value — fall through.
	}
	if v := os.Getenv("VOX_AI_MODEL"); v != "" {
		return v
	}
	if c.userCfg.AIModel != nil && *c.userCfg.AIModel != "" {
		return *c.userCfg.AIModel
	}
	return defaultModel
}

// AnthropicKey returns the Anthropic API key to use and which source it came from.
// Precedence: ANTHROPIC_API_KEY env > vox-anthropic-key LD flag > config file > empty.
//
// NOTE: The precedence here is intentionally INVERTED from other flag helpers
// (AIModel, boolFlag, etc.) which use LD > env > config. For a secret like an
// API key, a personal env-var override must beat the team-managed LD value so
// individual developers can use their own key when needed.
//
// The LD flag enables a team admin to set the key centrally so individual
// developers don't need personal API keys or Anthropic accounts.
func (c *Client) AnthropicKey() (key, source string) {
	// Env var always wins -- allows personal override.
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v, "env"
	}
	// LD flag -- team-managed key.
	if c.ld != nil {
		val, detail, _ := c.ld.StringVariationDetailCtx(
			context.Background(), KeyAnthropicKey, c.ctx, "",
		)
		if detail.Reason.GetKind() != ldreason.EvalReasonError && val != "" {
			return val, "ld_flag"
		}
	}
	// Config file.
	if c.userCfg.AnthropicKey != nil && *c.userCfg.AnthropicKey != "" {
		return *c.userCfg.AnthropicKey, "config_file"
	}
	return "", ""
}

// boolFlag evaluates a boolean flag with the precedence chain:
// LD flag > env var > config file pointer > hardcoded default.
//
// When the LD client is connected, we use BoolVariationDetailCtx to
// distinguish "flag evaluated successfully" from "flag not found / error."
// On error (flag missing, client not ready, etc.) we fall through to
// env var and config file instead of returning the SDK's default.
func (c *Client) boolFlag(key, envVar string, cfgVal *bool, defaultVal bool) bool {
	if c.ld != nil {
		val, detail, _ := c.ld.BoolVariationDetailCtx(
			context.Background(), key, c.ctx, defaultVal,
		)
		if detail.Reason.GetKind() != ldreason.EvalReasonError {
			// Flag was found and evaluated — use the LD result.
			return val
		}
		// Flag not found or evaluation error — fall through to env/config.
	}
	if v := os.Getenv(envVar); v != "" {
		return parseBool(v)
	}
	if cfgVal != nil {
		return *cfgVal
	}
	return defaultVal
}

func parseBool(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
