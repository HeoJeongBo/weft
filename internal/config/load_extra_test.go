package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knadh/koanf/v2"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadUserFile(t *testing.T) {
	dir := t.TempDir()
	user := writeFile(t, dir, "config.yaml", "branch:\n  prefix: \"user/\"\n")

	c, err := Load(Sources{UserPath: user})
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch.Prefix != "user/" {
		t.Errorf("user file not applied: prefix = %q", c.Branch.Prefix)
	}
	// Untouched fields keep defaults.
	if c.Claude.Command != "claude" {
		t.Errorf("default lost: claude command = %q", c.Claude.Command)
	}
}

func TestLoadUserThenProjectLayering(t *testing.T) {
	dir := t.TempDir()
	user := writeFile(t, dir, "config.yaml", "branch:\n  prefix: \"user/\"\ntmux:\n  window: \"{name}-u\"\n")
	proj := writeFile(t, dir, "weft.yaml", "branch:\n  prefix: \"proj/\"\n")

	c, err := Load(Sources{UserPath: user, ProjectPath: proj})
	if err != nil {
		t.Fatal(err)
	}
	// Project wins over user for prefix...
	if c.Branch.Prefix != "proj/" {
		t.Errorf("project should override user: prefix = %q", c.Branch.Prefix)
	}
	// ...but user value survives where project is silent.
	if c.Tmux.Window != "{name}-u" {
		t.Errorf("user value lost: tmux window = %q", c.Tmux.Window)
	}
}

func TestLoadMissingFilesUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	// Point at paths that don't exist: the Exists guard skips them.
	c, err := Load(Sources{
		UserPath:    filepath.Join(dir, "nope-user.yaml"),
		ProjectPath: filepath.Join(dir, "nope-proj.yaml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch.Prefix != "weft/" {
		t.Errorf("defaults not used: prefix = %q", c.Branch.Prefix)
	}
}

func TestLoadMalformedUserFile(t *testing.T) {
	dir := t.TempDir()
	user := writeFile(t, dir, "config.yaml", "branch:\n  prefix: \"unterminated\n")

	_, err := Load(Sources{UserPath: user})
	if err == nil {
		t.Fatal("expected error for malformed user yaml")
	}
	if !strings.Contains(err.Error(), user) {
		t.Errorf("error should name the user path: %v", err)
	}
}

func TestLoadMalformedProjectFile(t *testing.T) {
	dir := t.TempDir()
	proj := writeFile(t, dir, "weft.yaml", "branch:\n  prefix: \"unterminated\n")

	_, err := Load(Sources{ProjectPath: proj})
	if err == nil {
		t.Fatal("expected error for malformed project yaml")
	}
	if !strings.Contains(err.Error(), proj) {
		t.Errorf("error should name the project path: %v", err)
	}
}

func TestLoadMalformedOverrideFile(t *testing.T) {
	dir := t.TempDir()
	override := writeFile(t, dir, "other.yaml", "branch: [unterminated\n")

	_, err := Load(Sources{OverridePath: override})
	if err == nil {
		t.Fatal("expected error for malformed override yaml")
	}
	if !strings.Contains(err.Error(), override) {
		t.Errorf("error should name the override path: %v", err)
	}
}

func TestLoadUnmarshalTypeError(t *testing.T) {
	dir := t.TempDir()
	proj := writeFile(t, dir, "weft.yaml", "version: \"not-an-int\"\n")

	_, err := Load(Sources{ProjectPath: proj})
	if err == nil {
		t.Fatal("expected unmarshal error for non-int version")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Errorf("expected parse config error, got: %v", err)
	}
}

func TestLoadDefaultsError(t *testing.T) {
	sentinel := errors.New("boom")
	orig := loadDefaults
	loadDefaults = func(k *koanf.Koanf) error { return sentinel }
	t.Cleanup(func() { loadDefaults = orig })

	_, err := Load(Sources{})
	if err == nil {
		t.Fatal("expected error when loadDefaults fails")
	}
	if !strings.Contains(err.Error(), "load defaults") {
		t.Errorf("expected load defaults error, got: %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error should wrap sentinel, got: %v", err)
	}
}

func TestExpandTildeOnlyNoTokens(t *testing.T) {
	// No vars, no tokens: returns input unchanged (aside from tilde).
	got := Expand("plain/path", nil)
	if got != "plain/path" {
		t.Errorf("Expand with nil vars = %q, want plain/path", got)
	}
	// Bare tilde expands to home.
	home := Expand("~", map[string]string{})
	if strings.HasPrefix(home, "~") {
		t.Errorf("bare tilde not expanded: %q", home)
	}
}

func TestClaudeProcessNamesDedup(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{"custom-command", "claude-code", []string{"claude-code", "claude", "node"}},
		{"command-is-claude", "claude", []string{"claude", "node"}},
		{"command-is-node", "node", []string{"node", "claude"}},
		{"empty-command", "", []string{"claude", "node"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{Claude: ClaudeCfg{Command: tt.command}}
			got := c.ClaudeProcessNames()
			if len(got) != len(tt.want) {
				t.Fatalf("ClaudeProcessNames() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ClaudeProcessNames()[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}
