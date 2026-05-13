package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"vox/internal/userconfig"
)

func runConfig() {
	cfg, err := userconfig.Load()
	if err != nil {
		fmt.Printf("Warning: could not load existing config: %v\n", err)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Vox 2.0 Configuration")
	fmt.Println("=====================")
	fmt.Println("Configure which features are enabled by default.")
	fmt.Println("These can be overridden by env vars or LaunchDarkly flags.")
	fmt.Println()

	cfg.AIPostProcess = userconfig.BoolPtr(promptBool(reader, "AI post-processing (Claude cleans up grammar/punctuation)", cfg.AIPostProcess))
	cfg.PromptMode = userconfig.BoolPtr(promptBool(reader, "Prompt mode (\"summarize my clipboard\", etc.)", cfg.PromptMode))
	cfg.VoiceCommands = userconfig.BoolPtr(promptBool(reader, "Voice commands (\"create PR\", \"query flag\", etc.)", cfg.VoiceCommands))
	cfg.ContextAware = userconfig.BoolPtr(promptBool(reader, "Context-aware formatting (adjust output per app)", cfg.ContextAware))
	cfg.StreamingOverlay = userconfig.BoolPtr(promptBool(reader, "Streaming overlay (real-time transcription display)", cfg.StreamingOverlay))
	cfg.AIModel = userconfig.StringPtr(promptString(reader, "AI model", currentString(cfg.AIModel, "claude-haiku-4-5-20251001")))

	if err := userconfig.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	if path, err := userconfig.Path(); err == nil {
		fmt.Printf("Saved to %s\n", path)
	} else {
		fmt.Println("Saved.")
	}
	fmt.Println()
	fmt.Println("Note: Set VOX_LD_SDK_KEY to enable LaunchDarkly flag overrides.")
	fmt.Println("Note: AI features require an Anthropic API key from any source:")
	fmt.Println("  ANTHROPIC_API_KEY env var, vox-anthropic-key LD flag, or anthropic_key in this config.")
}

func promptBool(reader *bufio.Reader, label string, current *bool) bool {
	def := "N"
	if current != nil && *current {
		def = "Y"
	}
	yn := "y/N"
	if def == "Y" {
		yn = "Y/n"
	}

	fmt.Printf("  %s? [%s]: ", label, yn)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))

	if line == "" {
		return def == "Y"
	}
	return line == "y" || line == "yes"
}

func promptString(reader *bufio.Reader, label, current string) string {
	fmt.Printf("  %s [%s]: ", label, current)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return current
	}
	return line
}

func currentString(p *string, defaultVal string) string {
	if p != nil && *p != "" {
		return *p
	}
	return defaultVal
}
