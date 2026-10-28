package format

import "vox/internal/appctx"

// Hint returns a formatting instruction string to append to a Claude
// system prompt based on the focused application category. This allows
// AI post-processing to produce output that fits the context -- code-like
// in terminals/IDEs, conversational in chat, structured in browsers.
//
// Returns empty string for unknown apps (no formatting hint).
func Hint(category appctx.Category) string {
	switch category {
	case appctx.CategoryTerminal:
		return "The user is in a terminal. Output should be plain text suitable for a command line. No markdown formatting. Keep it terse and technical."

	case appctx.CategoryIDE:
		return "The user is in a code editor. Output should be well-formatted for code context -- use proper technical terminology, code-friendly formatting, and be precise."

	case appctx.CategoryChat:
		return "The user is in a chat application (Slack, Discord, etc.). Keep it conversational and brief. Use short sentences. Avoid overly formal language."

	case appctx.CategoryBrowser:
		return "The user is in a browser (possibly Jira, Confluence, or a web app). Use structured formatting with clear sentences. Bullet points are okay where appropriate."

	case appctx.CategoryEditor:
		return "The user is in a text editor or document application. Output should be well-structured prose with proper grammar and formatting."

	default:
		return ""
	}
}

// SystemPromptWithHint returns a system prompt with an optional context-aware
// formatting hint appended. If the hint is empty (unknown app), returns the
// base prompt unchanged.
func SystemPromptWithHint(basePrompt string, category appctx.Category) string {
	hint := Hint(category)
	if hint == "" {
		return basePrompt
	}
	// Hints come last, so the model weighs them heavily; scope them to style
	// so "brief"/"terse" can't override the base prompt's no-dropping rule.
	return basePrompt + "\n\nFormatting context (adjust style only; never drop, shorten, or summarize content): " + hint
}
