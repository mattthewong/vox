package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const shellTimeout = 30 * time.Second

// maxOutputSize caps stdout/stderr capture to prevent unbounded memory growth.
const maxOutputSize = 1 << 20 // 1 MB

// allowedURLSchemes restricts which URL schemes open-url will accept.
var allowedURLSchemes = []string{"http://", "https://"}

// ShellCommand executes a shell command and returns its output.
type ShellCommand struct {
	name    string
	actions []string
	builder func(args string) (string, []string) // returns command and args
}

// NewShellCommand creates a command that matches the given actions and
// builds a shell command from the args. The builder must not be nil.
func NewShellCommand(name string, actions []string, builder func(args string) (string, []string)) *ShellCommand {
	if builder == nil {
		panic("commands: NewShellCommand requires a non-nil builder")
	}
	return &ShellCommand{name: name, actions: actions, builder: builder}
}

func (c *ShellCommand) Name() string { return c.name }

func (c *ShellCommand) Match(action string) bool {
	for _, a := range c.actions {
		if a == action {
			return true
		}
	}
	return false
}

func (c *ShellCommand) Execute(ctx context.Context, args string) (string, error) {
	cmdName, cmdArgs := c.builder(args)

	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.WaitDelay = 5 * time.Second // prevent hanging on child processes holding pipes

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdout, remaining: maxOutputSize}
	cmd.Stderr = &limitedWriter{buf: &stderr, remaining: maxOutputSize}

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s: %s: %w", c.name, errMsg, err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// isAllowedURL checks that the URL starts with an allowed scheme.
func isAllowedURL(u string) bool {
	lower := strings.ToLower(u)
	for _, scheme := range allowedURLSchemes {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

// limitedWriter wraps a bytes.Buffer and stops writing after a byte limit.
type limitedWriter struct {
	buf       *bytes.Buffer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return len(p), nil // discard silently
	}
	if int64(len(p)) > w.remaining {
		p = p[:w.remaining]
	}
	n, err := w.buf.Write(p)
	w.remaining -= int64(n)
	return n, err
}

// envOrDefault reads an environment variable, returning fallback if unset or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// DefaultCommands returns the built-in voice commands.
func DefaultCommands() []Command {
	return []Command{
		NewShellCommand("create-pr", []string{"create-pr"}, func(args string) (string, []string) {
			cmdArgs := []string{"pr", "create", "--fill"}
			if args != "" {
				cmdArgs = append(cmdArgs, "--title", args)
			}
			return "gh", cmdArgs
		}),

		NewShellCommand("list-issues", []string{"list-issues"}, func(args string) (string, []string) {
			return "gh", []string{"issue", "list", "--assignee", "@me", "--limit", "10"}
		}),

		NewShellCommand("query-flag", []string{"query-flag"}, func(args string) (string, []string) {
			if args == "" {
				return "echo", []string{"usage: query flag <flag-key>"}
			}
			project := envOrDefault("LD_PROJECT", "default")
			environment := envOrDefault("LD_ENVIRONMENT", "production")
			return "ldcli", []string{"flags", "get", "--flag", args, "--project", project, "--environment", environment}
		}),

		NewShellCommand("create-ticket", []string{"create-ticket"}, func(args string) (string, []string) {
			summary := "New ticket"
			if args != "" {
				summary = args
			}
			return "echo", []string{fmt.Sprintf("[TODO] Create Jira ticket: %s", summary)}
		}),

		NewShellCommand("git-commit-all", []string{"git-commit-all"}, func(args string) (string, []string) {
			if args == "" {
				return "echo", []string{"usage: commit all with message <message>"}
			}
			return "git", []string{"commit", "-am", args}
		}),

		NewShellCommand("git-commit", []string{"git-commit"}, func(args string) (string, []string) {
			if args == "" {
				return "echo", []string{"usage: commit with message <message>"}
			}
			return "git", []string{"commit", "-m", args}
		}),

		NewShellCommand("git-status", []string{"git-status"}, func(args string) (string, []string) {
			return "git", []string{"status", "--short", "--branch"}
		}),

		NewShellCommand("git-diff", []string{"git-diff"}, func(args string) (string, []string) {
			return "git", []string{"diff", "--stat"}
		}),

		NewShellCommand("git-push", []string{"git-push"}, func(args string) (string, []string) {
			cmdArgs := []string{"push"}
			if args != "" {
				cmdArgs = append(cmdArgs, strings.Fields(args)...)
			}
			return "git", cmdArgs
		}),

		NewShellCommand("git-pull", []string{"git-pull"}, func(args string) (string, []string) {
			cmdArgs := []string{"pull", "--ff-only"}
			if args != "" {
				cmdArgs = append(cmdArgs, strings.Fields(args)...)
			}
			return "git", cmdArgs
		}),

		NewShellCommand("run-tests", []string{"run-tests"}, func(args string) (string, []string) {
			if args != "" {
				if !strings.HasPrefix(args, "./") || strings.Contains(args, "..") {
					return "echo", []string{"usage: run test ./package/path"}
				}
				return "go", []string{"test", "-v", args}
			}
			return "go", []string{"test", "./..."}
		}),

		NewShellCommand("open-url", []string{"open-url"}, func(args string) (string, []string) {
			if args == "" {
				return "echo", []string{"usage: open <url>"}
			}
			if !isAllowedURL(args) {
				return "echo", []string{fmt.Sprintf("refused to open %q: only http:// and https:// URLs are allowed", args)}
			}
			return "open", []string{"--", args}
		}),
	}
}
