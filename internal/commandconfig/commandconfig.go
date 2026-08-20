package commandconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CommandDef represents a user-defined voice command from YAML.
type CommandDef struct {
	Name            string   `yaml:"name"`
	Triggers        []string `yaml:"triggers"`
	Command         string   `yaml:"command"`
	Args            []string `yaml:"args,omitempty"`
	ForwardArgs     bool     `yaml:"forward_args,omitempty"`
	RequireDotSlash bool     `yaml:"require_dot_slash,omitempty"`
	Usage           string   `yaml:"usage,omitempty"`
}

// commandsFile is the top-level YAML structure.
type commandsFile struct {
	Commands []CommandDef `yaml:"commands"`
}

// DefaultPath returns the default path for the commands config file.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".vox", "commands.yaml")
	}
	return filepath.Join(home, ".vox", "commands.yaml")
}

// Load reads and parses a commands YAML file.
// Returns (nil, nil) if the file does not exist.
// Returns (nil, error) if the file exists but cannot be parsed.
func Load(path string) ([]CommandDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading commands config: %w", err)
	}

	var f commandsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing commands config: %w", err)
	}

	if err := Validate(f.Commands); err != nil {
		return nil, err
	}

	return f.Commands, nil
}

// Validate checks that all command definitions have required fields
// and that there are no duplicate names.
func Validate(defs []CommandDef) error {
	seenNames := make(map[string]bool, len(defs))
	seenTriggers := make(map[string]string) // trigger -> owning command name
	for i, d := range defs {
		if d.Name == "" {
			return fmt.Errorf("command %d: name is required", i)
		}
		if len(d.Triggers) == 0 {
			return fmt.Errorf("command %q: at least one trigger is required", d.Name)
		}
		for j, t := range d.Triggers {
			if strings.TrimSpace(t) == "" {
				return fmt.Errorf("command %q: trigger %d is empty or whitespace-only", d.Name, j)
			}
			lower := strings.ToLower(t)
			if owner, ok := seenTriggers[lower]; ok {
				return fmt.Errorf("command %q: trigger %q duplicates trigger in command %q", d.Name, t, owner)
			}
			seenTriggers[lower] = d.Name
		}
		if d.Command == "" {
			return fmt.Errorf("command %q: command is required", d.Name)
		}
		if seenNames[d.Name] {
			return fmt.Errorf("command %q: duplicate name", d.Name)
		}
		seenNames[d.Name] = true
	}
	return nil
}
