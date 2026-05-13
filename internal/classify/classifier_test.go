package classify

import "testing"

func TestClassifyPromptMode(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantMode    Mode
		wantAction  string
		wantSubject string
		wantRawArgs string
	}{
		{"summarize clipboard", "summarize my clipboard", ModePrompt, "summarize", "clipboard", ""},
		{"summarize clipboard alt", "Summarize clipboard", ModePrompt, "summarize", "clipboard", ""},
		{"summarize this", "summarize this", ModePrompt, "summarize", "this", ""},
		{"summarize freeform", "summarize the meeting notes from today", ModePrompt, "summarize", "the meeting notes from today", "the meeting notes from today"},
		{"explain error", "explain this error", ModePrompt, "explain", "error", ""},
		{"explain this", "Explain this", ModePrompt, "explain", "this", ""},
		{"explain freeform", "explain what happened in the last deploy", ModePrompt, "explain", "what happened in the last deploy", "what happened in the last deploy"},
		{"rewrite as commit", "rewrite this as a commit message", ModePrompt, "rewrite", "a commit message", "a commit message"},
		{"rewrite as", "rewrite as a haiku", ModePrompt, "rewrite", "a haiku", "a haiku"},
		{"translate to", "translate to Spanish", ModePrompt, "translate", "Spanish", "Spanish"},
		{"translate this to", "translate this to French", ModePrompt, "translate", "French", "French"},
		{"fix grammar", "fix the grammar", ModePrompt, "fix-grammar", "", ""},
		{"proofread", "proofread this", ModePrompt, "proofread", "this", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.input)
			if got.Mode != tt.wantMode {
				t.Errorf("Classify(%q).Mode = %v, want %v", tt.input, got.Mode, tt.wantMode)
			}
			if got.Action != tt.wantAction {
				t.Errorf("Classify(%q).Action = %q, want %q", tt.input, got.Action, tt.wantAction)
			}
			if got.Subject != tt.wantSubject {
				t.Errorf("Classify(%q).Subject = %q, want %q", tt.input, got.Subject, tt.wantSubject)
			}
			if got.RawArgs != tt.wantRawArgs {
				t.Errorf("Classify(%q).RawArgs = %q, want %q", tt.input, got.RawArgs, tt.wantRawArgs)
			}
		})
	}
}

func TestClassifyCommandMode(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAction string
		wantArgs   string
	}{
		{"hey vox", "hey vox do something", "vox", "do something"},
		{"vox prefix", "vox check the logs", "vox", "check the logs"},
		{"create pr full", "create a pull request for the bugfix", "create-pr", "for the bugfix"},
		{"create pr exact", "create pr", "create-pr", ""},
		{"create a pr exact", "create a pr", "create-pr", ""},
		{"create a pr with args", "create a pr for the bugfix", "create-pr", "for the bugfix"},
		{"open a pr exact", "open a pr", "create-pr", ""},
		{"open a pr with args", "open a pr for feature-x", "create-pr", "for feature-x"},
		{"open pr exact", "open pr", "create-pr", ""},
		{"list issues", "list my issues", "list-issues", ""},
		{"show issues", "show issues", "list-issues", ""},
		{"create ticket", "create a ticket for the auth bug", "create-ticket", "for the auth bug"},
		{"create jira", "create jira", "create-ticket", ""},
		{"query flag", "query flag enable-new-checkout", "query-flag", "enable-new-checkout"},
		{"check flag", "check flag dark-mode", "query-flag", "dark-mode"},
		{"flag status", "flag status my-feature", "query-flag", "my-feature"},
		{"commit all with message", "commit all with message fix everything", "git-commit-all", "fix everything"},
		{"commit all", "commit all updated readme", "git-commit-all", "updated readme"},
		{"commit with message", "commit with message fix the login bug", "git-commit", "fix the login bug"},
		{"git commit", "git commit updated readme", "git-commit", "updated readme"},
		{"git status", "git status", "git-status", ""},
		{"show git status", "show git status", "git-status", ""},
		{"git diff", "git diff", "git-diff", ""},
		{"show diff", "show diff", "git-diff", ""},
		{"git push", "git push", "git-push", ""},
		{"push my changes", "push my changes", "git-push", ""},
		{"git pull", "git pull", "git-pull", ""},
		{"pull from remote", "pull from remote", "git-pull", ""},
		{"pull latest changes", "pull latest changes", "git-pull", ""},
		{"run tests", "run tests", "run-tests", ""},
		{"run the tests", "run the tests", "run-tests", ""},
		{"run test specific", "run test ./internal/flags/", "run-tests", "./internal/flags/"},
		{"open https url", "open https://example.com", "open-url", "example.com"},
		{"open http url", "open http://localhost:8080/api", "open-url", "localhost:8080/api"},
		{"open localhost with port", "open localhost:3000", "open-url", "3000"},
		{"open localhost exact", "open localhost", "open-url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.input)
			if got.Mode != ModeCommand {
				t.Errorf("Classify(%q).Mode = %v, want command", tt.input, got.Mode)
			}
			if got.Action != tt.wantAction {
				t.Errorf("Classify(%q).Action = %q, want %q", tt.input, got.Action, tt.wantAction)
			}
			if got.RawArgs != tt.wantArgs {
				t.Errorf("Classify(%q).RawArgs = %q, want %q", tt.input, got.RawArgs, tt.wantArgs)
			}
		})
	}
}

func TestClassifyDictation(t *testing.T) {
	// These should all be classified as dictation -- they contain words that
	// look like commands/prompts but aren't in the right position or context.
	inputs := []string{
		// Mid-sentence trigger words
		"I want to explain something to my team",
		"Let me summarize what happened yesterday in my standup",
		"The summary of the meeting was good",
		"Can you help me create a presentation",
		"We should translate the requirements into code",
		"I was explaining the architecture to the new engineer",
		"There's a race condition in the drain path where WaitOrCancel interleaves with channel close during shutdown",
		"Hello world",
		"This is just regular dictation text",
		"",
		"The create PR workflow is broken",
		"I need to query the database for old records",
		"Check if the deployment is done",
		// Word-boundary: "open a pr..." that isn't about pull requests
		"open a presentation for tomorrow",
		"open a preview of the site",
		"open a private browsing window",
		// Word-boundary: "create a pr..." / "create pr..." that isn't about PRs
		"create a presentation deck",
		"create problems for the team",
		// Word-boundary: git/test phrases in natural dictation
		"push changes to the next sprint",
		"pull latest reports from the dashboard",
		"pull changes from the staging branch and review them",
		"push changes to the review board",
		"commit message looks wrong to me",
		"the commit message is too long",
		"show different options to the user",
		// "open" without a URL should be dictation
		"open the door",
		"open source software is great",
		"open question about the design",
		"open settings",
		"open a file",
		"open the discussion about pricing",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			got := Classify(input)
			if got.Mode != ModeDictation {
				t.Errorf("Classify(%q) = %v (action=%q), want dictation", input, got.Mode, got.Action)
			}
		})
	}
}

func TestClassifyCaseInsensitive(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"SUMMARIZE MY CLIPBOARD", ModePrompt},
		{"Summarize My Clipboard", ModePrompt},
		{"CREATE PR", ModeCommand},
		{"Hey Vox do something", ModeCommand},
	}
	for _, tt := range tests {
		got := Classify(tt.input)
		if got.Mode != tt.want {
			t.Errorf("Classify(%q).Mode = %v, want %v", tt.input, got.Mode, tt.want)
		}
	}
}

func TestClassifyPreservesOriginalCase(t *testing.T) {
	got := Classify("Summarize My Important Meeting Notes")
	if got.RawArgs != "My Important Meeting Notes" {
		t.Errorf("RawArgs = %q, want original case preserved", got.RawArgs)
	}
}

func TestClassifyWhitespace(t *testing.T) {
	got := Classify("  summarize my clipboard  ")
	if got.Mode != ModePrompt {
		t.Errorf("leading/trailing whitespace should be trimmed; got mode %v", got.Mode)
	}
	if got.Action != "summarize" {
		t.Errorf("Action = %q, want %q", got.Action, "summarize")
	}
	if got.Subject != "clipboard" {
		t.Errorf("Subject = %q, want %q", got.Subject, "clipboard")
	}
	if got.RawArgs != "" {
		t.Errorf("RawArgs = %q, want empty (exact prefix match)", got.RawArgs)
	}
}

func TestModeString(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeDictation, "dictation"},
		{ModeCommand, "command"},
		{ModePrompt, "prompt"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("Mode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}
