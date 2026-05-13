package commands

import (
	"context"
	"strings"
	"testing"
)

func nopBuilder(args string) (string, []string) { return "echo", nil }

func TestShellCommandMatch(t *testing.T) {
	cmd := NewShellCommand("test", []string{"do-it", "also-do-it"}, nopBuilder)

	if !cmd.Match("do-it") {
		t.Error("should match do-it")
	}
	if !cmd.Match("also-do-it") {
		t.Error("should match also-do-it")
	}
	if cmd.Match("nope") {
		t.Error("should not match nope")
	}
}

func TestShellCommandName(t *testing.T) {
	cmd := NewShellCommand("my-cmd", nil, nopBuilder)
	if cmd.Name() != "my-cmd" {
		t.Errorf("name = %q", cmd.Name())
	}
}

func TestShellCommandExecuteEcho(t *testing.T) {
	cmd := NewShellCommand("echo-test", []string{"echo"}, func(args string) (string, []string) {
		return "echo", []string{"hello", args}
	})

	result, err := cmd.Execute(context.Background(), "world")
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestShellCommandExecuteFailure(t *testing.T) {
	cmd := NewShellCommand("fail-test", []string{"fail"}, func(args string) (string, []string) {
		return "false", nil // `false` always exits 1
	})

	_, err := cmd.Execute(context.Background(), "")
	if err == nil {
		t.Fatal("expected error from false command")
	}
}

func TestDefaultCommandsRegistered(t *testing.T) {
	cmds := DefaultCommands()
	if len(cmds) == 0 {
		t.Fatal("no default commands registered")
	}

	names := make(map[string]bool)
	for _, cmd := range cmds {
		names[cmd.Name()] = true
	}

	expected := []string{"create-pr", "list-issues", "query-flag", "create-ticket", "open-url", "git-commit-all", "git-commit", "git-status", "git-diff", "git-push", "git-pull", "run-tests"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing default command: %s", name)
		}
	}
}

func TestDefaultCommandsMatch(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	actions := []string{"create-pr", "list-issues", "query-flag", "create-ticket", "open-url", "git-commit-all", "git-commit", "git-status", "git-diff", "git-push", "git-pull", "run-tests"}
	for _, action := range actions {
		found := false
		for _, cmd := range r.commands {
			if cmd.Match(action) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no command matches action %q", action)
		}
	}
}

func TestCreateTicketPlaceholder(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	result, err := r.Execute(context.Background(), "create-ticket", "fix the auth bug")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "fix the auth bug") {
		t.Errorf("result should contain ticket summary, got %q", result)
	}
}

func TestQueryFlagNoArgs(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	// Empty args takes the echo path, not the ldcli path.
	result, err := r.Execute(context.Background(), "query-flag", "")
	if err != nil {
		t.Fatalf("empty args should use echo path: %v", err)
	}
	if !strings.Contains(result, "usage: query flag") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestOpenURLAllowsHTTPS(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	// We can't actually call `open` in CI, but we can verify the builder
	// produces the right command by testing the disallowed path.
	result, err := r.Execute(context.Background(), "open-url", "file:///etc/passwd")
	if err != nil {
		t.Fatalf("disallowed URL should echo refusal, not error: %v", err)
	}
	if !strings.Contains(result, "refused to open") {
		t.Errorf("expected refusal message for file:// URL, got %q", result)
	}
}

func TestOpenURLRejectsFlagInjection(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	result, err := r.Execute(context.Background(), "open-url", "-e")
	if err != nil {
		t.Fatalf("flag-like arg should echo refusal, not error: %v", err)
	}
	if !strings.Contains(result, "refused to open") {
		t.Errorf("expected refusal for flag-like arg, got %q", result)
	}
}

func TestOpenURLRejectsCustomSchemes(t *testing.T) {
	schemes := []string{"ssh://attacker.com", "tel:+1234567890", "ftp://example.com", "/etc/passwd"}
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	for _, scheme := range schemes {
		result, err := r.Execute(context.Background(), "open-url", scheme)
		if err != nil {
			t.Fatalf("disallowed URL %q should echo refusal: %v", scheme, err)
		}
		if !strings.Contains(result, "refused to open") {
			t.Errorf("expected refusal for %q, got %q", scheme, result)
		}
	}
}

func TestGitCommitAllNoArgs(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	result, err := r.Execute(context.Background(), "git-commit-all", "")
	if err != nil {
		t.Fatalf("empty args should use echo path: %v", err)
	}
	if !strings.Contains(result, "usage: commit all with message") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestGitCommitAllUsesAM(t *testing.T) {
	// Verify git-commit-all uses -am to auto-stage all tracked files.
	cmd := findCommand(DefaultCommands(), "git-commit-all")
	if cmd == nil {
		t.Fatal("git-commit-all command not found")
	}
	sc := cmd.(*ShellCommand)
	cmdName, cmdArgs := sc.builder("fix everything")
	if cmdName != "git" {
		t.Errorf("cmdName = %q, want git", cmdName)
	}
	want := []string{"commit", "-am", "fix everything"}
	if len(cmdArgs) != len(want) {
		t.Fatalf("cmdArgs = %v, want %v", cmdArgs, want)
	}
	for i := range want {
		if cmdArgs[i] != want[i] {
			t.Errorf("cmdArgs[%d] = %q, want %q", i, cmdArgs[i], want[i])
		}
	}
}

func TestGitCommitNoArgs(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	result, err := r.Execute(context.Background(), "git-commit", "")
	if err != nil {
		t.Fatalf("empty args should use echo path: %v", err)
	}
	if !strings.Contains(result, "usage: commit with message") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestGitCommitUsesMessageOnly(t *testing.T) {
	// Verify the builder produces -m (not -am) so only staged files are committed.
	cmd := findCommand(DefaultCommands(), "git-commit")
	if cmd == nil {
		t.Fatal("git-commit command not found")
	}
	sc := cmd.(*ShellCommand)
	cmdName, cmdArgs := sc.builder("fix the bug")
	if cmdName != "git" {
		t.Errorf("cmdName = %q, want git", cmdName)
	}
	want := []string{"commit", "-m", "fix the bug"}
	if len(cmdArgs) != len(want) {
		t.Fatalf("cmdArgs = %v, want %v", cmdArgs, want)
	}
	for i := range want {
		if cmdArgs[i] != want[i] {
			t.Errorf("cmdArgs[%d] = %q, want %q", i, cmdArgs[i], want[i])
		}
	}
}

func TestGitPullUsesFFOnly(t *testing.T) {
	cmd := findCommand(DefaultCommands(), "git-pull")
	if cmd == nil {
		t.Fatal("git-pull command not found")
	}
	sc := cmd.(*ShellCommand)
	cmdName, cmdArgs := sc.builder("")
	if cmdName != "git" {
		t.Errorf("cmdName = %q, want git", cmdName)
	}
	if len(cmdArgs) < 2 || cmdArgs[1] != "--ff-only" {
		t.Errorf("cmdArgs = %v, want [pull --ff-only]", cmdArgs)
	}
}

func TestGitPushForwardsArgs(t *testing.T) {
	cmd := findCommand(DefaultCommands(), "git-push")
	if cmd == nil {
		t.Fatal("git-push command not found")
	}
	sc := cmd.(*ShellCommand)

	// No args: just "push"
	_, argsNoExtra := sc.builder("")
	if len(argsNoExtra) != 1 || argsNoExtra[0] != "push" {
		t.Errorf("no-arg push: cmdArgs = %v, want [push]", argsNoExtra)
	}

	// With args: "push origin main"
	_, argsWithExtra := sc.builder("origin main")
	want := []string{"push", "origin", "main"}
	if len(argsWithExtra) != len(want) {
		t.Fatalf("with-arg push: cmdArgs = %v, want %v", argsWithExtra, want)
	}
	for i := range want {
		if argsWithExtra[i] != want[i] {
			t.Errorf("cmdArgs[%d] = %q, want %q", i, argsWithExtra[i], want[i])
		}
	}
}

func TestRunTestsNoArgs(t *testing.T) {
	cmd := findCommand(DefaultCommands(), "run-tests")
	if cmd == nil {
		t.Fatal("run-tests command not found")
	}
	sc := cmd.(*ShellCommand)
	cmdName, cmdArgs := sc.builder("")
	if cmdName != "go" {
		t.Errorf("cmdName = %q, want go", cmdName)
	}
	if len(cmdArgs) != 2 || cmdArgs[0] != "test" || cmdArgs[1] != "./..." {
		t.Errorf("cmdArgs = %v, want [test ./...]", cmdArgs)
	}
}

func TestRunTestsWithPath(t *testing.T) {
	cmd := findCommand(DefaultCommands(), "run-tests")
	if cmd == nil {
		t.Fatal("run-tests command not found")
	}
	sc := cmd.(*ShellCommand)
	cmdName, cmdArgs := sc.builder("./internal/flags/")
	if cmdName != "go" {
		t.Errorf("cmdName = %q, want go", cmdName)
	}
	if len(cmdArgs) != 3 || cmdArgs[0] != "test" || cmdArgs[1] != "-v" || cmdArgs[2] != "./internal/flags/" {
		t.Errorf("cmdArgs = %v, want [test -v ./internal/flags/]", cmdArgs)
	}
}

func TestRunTestsRejectsTraversal(t *testing.T) {
	cmds := DefaultCommands()
	r := NewRegistry(cmds...)

	tests := []string{
		"../../other-repo/...",
		"/etc/passwd",
		"something-random",
	}
	for _, input := range tests {
		result, err := r.Execute(context.Background(), "run-tests", input)
		if err != nil {
			t.Fatalf("run-tests %q should use echo path: %v", input, err)
		}
		if !strings.Contains(result, "usage: run test ./package/path") {
			t.Errorf("run-tests %q should show usage, got %q", input, result)
		}
	}
}

// findCommand returns the command with the given name, or nil.
func findCommand(cmds []Command, name string) Command {
	for _, cmd := range cmds {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

func TestShellCommandContextCancellation(t *testing.T) {
	cmd := NewShellCommand("sleep-test", []string{"sleep"}, func(args string) (string, []string) {
		return "sleep", []string{"60"}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := cmd.Execute(ctx, "")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestNewShellCommandNilBuilderPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil builder")
		}
	}()
	NewShellCommand("bad", []string{"bad"}, nil)
}
