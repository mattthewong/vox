package commandconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.yaml")
	yaml := `commands:
  - name: deploy
    triggers: ["deploy", "ship it"]
    command: ./deploy.sh
    args: ["--env", "prod"]
  - name: test
    triggers: ["run tests"]
    command: go
    args: ["test", "./..."]
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	defs, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}

	if defs[0].Name != "deploy" {
		t.Errorf("expected name deploy, got %q", defs[0].Name)
	}
	if len(defs[0].Triggers) != 2 || defs[0].Triggers[0] != "deploy" || defs[0].Triggers[1] != "ship it" {
		t.Errorf("unexpected triggers: %v", defs[0].Triggers)
	}
	if defs[0].Command != "./deploy.sh" {
		t.Errorf("expected command ./deploy.sh, got %q", defs[0].Command)
	}
	if len(defs[0].Args) != 2 || defs[0].Args[0] != "--env" || defs[0].Args[1] != "prod" {
		t.Errorf("unexpected args: %v", defs[0].Args)
	}

	if defs[1].Name != "test" {
		t.Errorf("expected name test, got %q", defs[1].Name)
	}
	if defs[1].Command != "go" {
		t.Errorf("expected command go, got %q", defs[1].Command)
	}
}

func TestLoadMissingFile(t *testing.T) {
	defs, err := Load("/nonexistent/path/commands.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if defs != nil {
		t.Fatalf("expected nil defs for missing file, got: %v", defs)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.yaml")
	if err := os.WriteFile(path, []byte("{{{{not yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	defs, err := Load(path)
	if err == nil {
		t.Fatalf("expected error for invalid YAML, got defs: %v", defs)
	}
}

func TestLoadAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.yaml")
	yaml := `commands:
  - name: build
    triggers: ["build"]
    command: make
    args: ["all"]
    forward_args: true
    require_dot_slash: true
    usage: "Build the project with optional extra args"
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	defs, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}

	d := defs[0]
	if !d.ForwardArgs {
		t.Error("expected forward_args to be true")
	}
	if !d.RequireDotSlash {
		t.Error("expected require_dot_slash to be true")
	}
	if d.Usage != "Build the project with optional extra args" {
		t.Errorf("unexpected usage: %q", d.Usage)
	}
}

func TestValidateMissingName(t *testing.T) {
	defs := []CommandDef{{
		Triggers: []string{"go"},
		Command:  "echo",
	}}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateMissingTriggers(t *testing.T) {
	defs := []CommandDef{{
		Name:    "foo",
		Command: "echo",
	}}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for missing triggers")
	}
}

func TestValidateEmptyTrigger(t *testing.T) {
	defs := []CommandDef{{
		Name:     "foo",
		Triggers: []string{"valid", ""},
		Command:  "echo",
	}}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for empty trigger")
	}
}

func TestValidateMissingCommand(t *testing.T) {
	defs := []CommandDef{{
		Name:     "foo",
		Triggers: []string{"do it"},
	}}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestValidateDuplicateNames(t *testing.T) {
	defs := []CommandDef{
		{Name: "foo", Triggers: []string{"a"}, Command: "echo"},
		{Name: "foo", Triggers: []string{"b"}, Command: "echo"},
	}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for duplicate names")
	}
}

func TestValidateValid(t *testing.T) {
	defs := []CommandDef{
		{Name: "deploy", Triggers: []string{"deploy", "ship"}, Command: "./deploy.sh"},
		{Name: "test", Triggers: []string{"run tests"}, Command: "go", Args: []string{"test", "./..."}},
	}
	if err := Validate(defs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWhitespaceOnlyTrigger(t *testing.T) {
	defs := []CommandDef{{
		Name:     "foo",
		Triggers: []string{"  "},
		Command:  "echo",
	}}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for whitespace-only trigger")
	}
}

func TestValidateDuplicateTriggers(t *testing.T) {
	defs := []CommandDef{
		{Name: "foo", Triggers: []string{"do thing"}, Command: "echo"},
		{Name: "bar", Triggers: []string{"do thing"}, Command: "echo"},
	}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for duplicate triggers across commands")
	}
}

func TestValidateDuplicateTriggersWithinCommand(t *testing.T) {
	defs := []CommandDef{
		{Name: "foo", Triggers: []string{"do thing", "do thing"}, Command: "echo"},
	}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for duplicate triggers within same command")
	}
}

func TestValidateDuplicateTriggersCaseInsensitive(t *testing.T) {
	defs := []CommandDef{
		{Name: "foo", Triggers: []string{"Deploy"}, Command: "echo"},
		{Name: "bar", Triggers: []string{"deploy"}, Command: "echo"},
	}
	if err := Validate(defs); err == nil {
		t.Fatal("expected error for case-insensitive duplicate triggers")
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if !filepath.IsAbs(p) {
		t.Errorf("expected absolute path, got %q", p)
	}
	if filepath.Base(p) != "commands.yaml" {
		t.Errorf("expected path to end with commands.yaml, got %q", p)
	}
}
