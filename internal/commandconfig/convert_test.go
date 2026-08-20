package commandconfig

import (
	"context"
	"strings"
	"testing"

	"vox/internal/classify"
	"vox/internal/commands"
)

func TestToCommandBasicExecution(t *testing.T) {
	def := CommandDef{
		Name:     "hello",
		Triggers: []string{"say hello"},
		Command:  "echo",
		Args:     []string{"hello", "world"},
	}

	cmd := ToCommand(def)

	if cmd.Name() != "hello" {
		t.Errorf("expected name %q, got %q", "hello", cmd.Name())
	}

	if !cmd.Match("hello") {
		t.Error("expected Match(\"hello\") to be true")
	}
	if cmd.Match("goodbye") {
		t.Error("expected Match(\"goodbye\") to be false")
	}

	out, err := cmd.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", out)
	}
}

func TestToCommandForwardArgs(t *testing.T) {
	def := CommandDef{
		Name:        "greeter",
		Triggers:    []string{"greet"},
		Command:     "echo",
		Args:        []string{"hello"},
		ForwardArgs: true,
	}

	cmd := ToCommand(def)
	out, err := cmd.Execute(context.Background(), "dear friend")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello dear friend" {
		t.Errorf("expected %q, got %q", "hello dear friend", out)
	}
}

func TestToCommandForwardArgsEmpty(t *testing.T) {
	def := CommandDef{
		Name:        "greeter",
		Triggers:    []string{"greet"},
		Command:     "echo",
		Args:        []string{"hello"},
		ForwardArgs: true,
	}

	cmd := ToCommand(def)
	out, err := cmd.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Errorf("expected %q, got %q", "hello", out)
	}
}

func TestToCommandUsageMessage(t *testing.T) {
	def := CommandDef{
		Name:        "deploy",
		Triggers:    []string{"deploy"},
		Command:     "./deploy.sh",
		Args:        []string{"--env", "prod"},
		ForwardArgs: true,
		Usage:       "usage: deploy <target>",
	}

	cmd := ToCommand(def)
	out, err := cmd.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "usage: deploy <target>" {
		t.Errorf("expected usage message, got %q", out)
	}
}

func TestToCommandRequireDotSlashValid(t *testing.T) {
	def := CommandDef{
		Name:            "run",
		Triggers:        []string{"run"},
		Command:         "echo",
		Args:            []string{"running"},
		ForwardArgs:     true,
		RequireDotSlash: true,
	}

	cmd := ToCommand(def)
	out, err := cmd.Execute(context.Background(), "./internal/foo/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "./internal/foo/") {
		t.Errorf("expected output to contain ./internal/foo/, got %q", out)
	}
}

func TestToCommandRequireDotSlashRejectsTraversal(t *testing.T) {
	def := CommandDef{
		Name:            "run",
		Triggers:        []string{"run"},
		Command:         "echo",
		Args:            []string{"running"},
		ForwardArgs:     true,
		RequireDotSlash: true,
	}

	cmd := ToCommand(def)
	out, err := cmd.Execute(context.Background(), "../etc/passwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "must start with ./") || !strings.Contains(out, "..") {
		t.Errorf("expected rejection message mentioning ./ and .., got %q", out)
	}
}

func TestToCommandRequireDotSlashRejectsNoDotSlash(t *testing.T) {
	def := CommandDef{
		Name:            "run",
		Triggers:        []string{"run"},
		Command:         "echo",
		Args:            []string{"running"},
		ForwardArgs:     true,
		RequireDotSlash: true,
	}

	cmd := ToCommand(def)
	out, err := cmd.Execute(context.Background(), "internal/foo/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "must start with ./") {
		t.Errorf("expected rejection message mentioning ./, got %q", out)
	}
}

func TestToCommandRequireDotSlashRejectsSecondToken(t *testing.T) {
	def := CommandDef{
		Name:            "run",
		Triggers:        []string{"run"},
		Command:         "echo",
		Args:            []string{"running"},
		ForwardArgs:     true,
		RequireDotSlash: true,
	}

	cmd := ToCommand(def)
	// "./safe /etc/passwd" — first token is valid but second is not
	out, err := cmd.Execute(context.Background(), "./safe /etc/passwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "must start with ./") {
		t.Errorf("expected rejection of second token, got %q", out)
	}
}

func TestMergeMixedCaseTriggers(t *testing.T) {
	userDefs := []CommandDef{
		{Name: "deploy", Triggers: []string{"Deploy To Staging"}, Command: "echo"},
	}

	_, prefixes := Merge(nil, userDefs)

	// Trigger should be stored lowercased
	found := false
	for _, p := range prefixes {
		if p.Prefix == "deploy to staging" && p.Action == "deploy" {
			found = true
		}
		if p.Prefix == "Deploy To Staging" {
			t.Errorf("trigger stored with original case %q, should be lowercased", p.Prefix)
		}
	}
	if !found {
		t.Error("expected lowercased trigger 'deploy to staging' in prefixes")
	}
}

func TestMergeMixedCaseTriggersClassify(t *testing.T) {
	userDefs := []CommandDef{
		{Name: "deploy", Triggers: []string{"Deploy To Staging"}, Command: "echo"},
	}

	_, prefixes := Merge(nil, userDefs)
	cl := classify.NewClassifier(prefixes)

	// Should match regardless of input case
	got := cl.Classify("deploy to staging")
	if got.Mode != classify.ModeCommand || got.Action != "deploy" {
		t.Errorf("expected command/deploy, got mode=%v action=%q", got.Mode, got.Action)
	}

	got = cl.Classify("Deploy To Staging")
	if got.Mode != classify.ModeCommand || got.Action != "deploy" {
		t.Errorf("expected command/deploy for mixed case input, got mode=%v action=%q", got.Mode, got.Action)
	}
}

func TestMergeAdditive(t *testing.T) {
	builtins := commands.DefaultCommands()
	userDefs := []CommandDef{
		{
			Name:     "deploy",
			Triggers: []string{"deploy", "ship it"},
			Command:  "echo",
			Args:     []string{"deploying"},
		},
	}

	merged, prefixes := Merge(builtins, userDefs)

	// Should have all builtins + the new user command
	if len(merged) != len(builtins)+1 {
		t.Errorf("expected %d commands, got %d", len(builtins)+1, len(merged))
	}

	// New command should be findable
	found := false
	for _, cmd := range merged {
		if cmd.Name() == "deploy" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'deploy' command in merged list")
	}

	// Prefixes should include user triggers
	foundTrigger := false
	for _, p := range prefixes {
		if p.Prefix == "ship it" && p.Action == "deploy" {
			foundTrigger = true
			break
		}
	}
	if !foundTrigger {
		t.Error("expected to find 'ship it' trigger in prefixes")
	}
}

func TestMergeOverride(t *testing.T) {
	builtins := commands.DefaultCommands()
	userDefs := []CommandDef{
		{
			Name:     "git-push",
			Triggers: []string{"push code"},
			Command:  "echo",
			Args:     []string{"custom-push"},
		},
	}

	merged, _ := Merge(builtins, userDefs)

	// Should not have grown: one builtin replaced
	if len(merged) != len(builtins) {
		t.Errorf("expected %d commands (override, not additive), got %d", len(builtins), len(merged))
	}

	// Find the git-push command and verify it's the custom one
	for _, cmd := range merged {
		if cmd.Name() == "git-push" {
			out, err := cmd.Execute(context.Background(), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out != "custom-push" {
				t.Errorf("expected custom-push output, got %q", out)
			}
			return
		}
	}
	t.Error("expected to find 'git-push' command in merged list")
}

func TestMergeNilUserDefs(t *testing.T) {
	builtins := commands.DefaultCommands()

	merged, prefixes := Merge(builtins, nil)

	if len(merged) != len(builtins) {
		t.Errorf("expected %d commands, got %d", len(builtins), len(merged))
	}

	defaults := classify.DefaultCommandPrefixes()
	if len(prefixes) != len(defaults) {
		t.Errorf("expected %d prefixes, got %d", len(defaults), len(prefixes))
	}
}

func TestMergePrefixesSortedByLength(t *testing.T) {
	builtins := commands.DefaultCommands()
	userDefs := []CommandDef{
		{
			Name:     "deploy",
			Triggers: []string{"d", "deploy now please"},
			Command:  "echo",
		},
	}

	_, prefixes := Merge(builtins, userDefs)

	for i := 1; i < len(prefixes); i++ {
		if len(prefixes[i].Prefix) > len(prefixes[i-1].Prefix) {
			t.Errorf("prefixes not sorted by length descending: %q (%d) before %q (%d)",
				prefixes[i-1].Prefix, len(prefixes[i-1].Prefix),
				prefixes[i].Prefix, len(prefixes[i].Prefix))
			break
		}
	}
}

func TestMergeOverriddenPrefixesRemoved(t *testing.T) {
	builtins := commands.DefaultCommands()
	userDefs := []CommandDef{
		{
			Name:     "git-push",
			Triggers: []string{"push code"},
			Command:  "echo",
			Args:     []string{"custom-push"},
		},
	}

	_, prefixes := Merge(builtins, userDefs)

	// Old built-in prefixes for git-push should be gone
	oldPrefixes := []string{"git push", "push to remote", "push my changes"}
	for _, old := range oldPrefixes {
		for _, p := range prefixes {
			if p.Prefix == old && p.Action == "git-push" {
				t.Errorf("expected old prefix %q for git-push to be removed, but found it", old)
			}
		}
	}

	// New trigger should be present
	found := false
	for _, p := range prefixes {
		if p.Prefix == "push code" && p.Action == "git-push" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected new 'push code' trigger for git-push to be present")
	}
}
