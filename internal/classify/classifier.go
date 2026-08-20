package classify

import "strings"

// Mode represents the type of voice input.
type Mode int

const (
	// ModeDictation is standard speech-to-text dictation.
	ModeDictation Mode = iota
	// ModeCommand triggers a voice command (e.g., "create PR", "query flag").
	ModeCommand
	// ModePrompt triggers a Claude prompt shortcut (e.g., "summarize my clipboard").
	ModePrompt
)

// String returns a human-readable label for the mode.
func (m Mode) String() string {
	switch m {
	case ModeCommand:
		return "command"
	case ModePrompt:
		return "prompt"
	default:
		return "dictation"
	}
}

// Intent represents the classified intent of a voice input.
type Intent struct {
	Mode    Mode
	Action  string // e.g., "summarize", "explain", "create-pr"
	Subject string // e.g., "clipboard", "this error", "last commit"
	RawArgs string // Everything after the matched prefix
}

// promptPrefixes maps trigger phrases to (action, subject-hint) pairs.
// Order matters: longer/more-specific prefixes should come first.
var promptPrefixes = []struct {
	prefix  string
	action  string
	subject string
}{
	{"summarize my clipboard", "summarize", "clipboard"},
	{"summarize clipboard", "summarize", "clipboard"},
	{"summarize this", "summarize", "this"},
	{"summarize ", "summarize", ""},
	{"explain this error", "explain", "error"},
	{"explain this", "explain", "this"},
	{"explain ", "explain", ""},
	{"rewrite this as ", "rewrite", ""},
	{"rewrite as ", "rewrite", ""},
	{"rewrite this ", "rewrite", "this"},
	{"rewrite ", "rewrite", ""},
	{"translate this to ", "translate", ""},
	{"translate to ", "translate", ""},
	{"translate ", "translate", ""},
	{"fix the grammar", "fix-grammar", ""},
	{"fix grammar", "fix-grammar", ""},
	{"proofread this", "proofread", "this"},
	{"proofread ", "proofread", ""},
}

// PrefixEntry maps a trigger phrase to an action (and optional subject hint).
type PrefixEntry struct {
	Prefix  string
	Action  string
	Subject string // optional, used by prompt prefixes
}

// DefaultCommandPrefixes returns the built-in command prefix table.
// Prefixes ending with a trailing space consume that space and capture everything
// after it as RawArgs. Prefixes without a trailing space require a word boundary
// (end-of-string or space) to avoid matching partial words like "open a preview".
func DefaultCommandPrefixes() []PrefixEntry {
	return []PrefixEntry{
		{"hey vox ", "vox", ""},
		{"vox ", "vox", ""},
		{"create a pull request", "create-pr", ""},
		{"create pull request", "create-pr", ""},
		{"create a pr ", "create-pr", ""},
		{"create a pr", "create-pr", ""},
		{"create pr ", "create-pr", ""},
		{"create pr", "create-pr", ""},
		{"open a pr ", "create-pr", ""},
		{"open a pr", "create-pr", ""},
		{"open pr ", "create-pr", ""},
		{"open pr", "create-pr", ""},
		{"list issues", "list-issues", ""},
		{"list my issues", "list-issues", ""},
		{"show my issues", "list-issues", ""},
		{"show issues", "list-issues", ""},
		{"create a ticket", "create-ticket", ""},
		{"create ticket", "create-ticket", ""},
		{"create a jira", "create-ticket", ""},
		{"create jira", "create-ticket", ""},
		{"commit all with message ", "git-commit-all", ""},
		{"commit all ", "git-commit-all", ""},
		{"commit with message ", "git-commit", ""},
		{"git commit ", "git-commit", ""},
		{"git status", "git-status", ""},
		{"show git status", "git-status", ""},
		{"git diff", "git-diff", ""},
		{"show diff", "git-diff", ""},
		{"git push", "git-push", ""},
		{"push to remote", "git-push", ""},
		{"push my changes", "git-push", ""},
		{"git pull", "git-pull", ""},
		{"pull from remote", "git-pull", ""},
		{"pull latest changes", "git-pull", ""},
		{"run tests", "run-tests", ""},
		{"run the tests", "run-tests", ""},
		{"run test ", "run-tests", ""},
		{"query flag ", "query-flag", ""},
		{"check flag ", "query-flag", ""},
		{"get flag ", "query-flag", ""},
		{"flag status ", "query-flag", ""},
		{"open https://", "open-url", ""},
		{"open http://", "open-url", ""},
		{"open localhost:", "open-url", ""},
		{"open localhost", "open-url", ""},
	}
}

// isWordBoundary returns true if the character is a valid word boundary for
// command prefix matching. Whisper often appends punctuation (period, comma,
// exclamation, question mark) to transcribed text, so these count as boundaries
// alongside spaces.
func isWordBoundary(c byte) bool {
	return c == ' ' || c == '.' || c == ',' || c == '!' || c == '?'
}

// Classifier performs intent classification with configurable command prefixes.
type Classifier struct {
	promptPrefixes  []PrefixEntry
	commandPrefixes []PrefixEntry
}

// NewClassifier creates a Classifier with the given command prefixes.
// Prompt prefixes use the built-in defaults.
func NewClassifier(commandPrefixes []PrefixEntry) *Classifier {
	// Convert internal promptPrefixes to PrefixEntry
	pp := make([]PrefixEntry, len(promptPrefixes))
	for i, p := range promptPrefixes {
		pp[i] = PrefixEntry{Prefix: p.prefix, Action: p.action, Subject: p.subject}
	}
	return &Classifier{
		promptPrefixes:  pp,
		commandPrefixes: commandPrefixes,
	}
}

// Classify determines whether text is dictation, a prompt query, or a command.
func (cl *Classifier) Classify(text string) Intent {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	for _, p := range cl.promptPrefixes {
		if strings.HasPrefix(lower, p.Prefix) {
			rawArgs := strings.TrimSpace(text[len(p.Prefix):])
			subject := p.Subject
			if subject == "" && rawArgs != "" {
				subject = rawArgs
			}
			return Intent{
				Mode:    ModePrompt,
				Action:  p.Action,
				Subject: subject,
				RawArgs: rawArgs,
			}
		}
	}

	for _, c := range cl.commandPrefixes {
		if strings.HasPrefix(lower, c.Prefix) {
			rest := lower[len(c.Prefix):]
			lastChar := c.Prefix[len(c.Prefix)-1]
			needsBoundary := (lastChar >= 'a' && lastChar <= 'z') || (lastChar >= '0' && lastChar <= '9')
			if needsBoundary && rest != "" && !isWordBoundary(rest[0]) {
				continue
			}
			rawArgs := strings.TrimSpace(text[len(c.Prefix):])
			return Intent{
				Mode:    ModeCommand,
				Action:  c.Action,
				RawArgs: rawArgs,
			}
		}
	}

	return Intent{Mode: ModeDictation}
}
