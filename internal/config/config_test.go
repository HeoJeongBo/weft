package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	c, err := Load(Sources{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch.Prefix != "weft/" {
		t.Errorf("prefix = %q, want weft/", c.Branch.Prefix)
	}
	if c.Claude.Command != "claude" || !c.Claude.ExecInContainer {
		t.Errorf("claude defaults wrong: %+v", c.Claude)
	}
	if !c.Cleanup.RequireClean || c.Cleanup.DeleteBranch {
		t.Errorf("cleanup defaults wrong: %+v", c.Cleanup)
	}
	if !c.Devcontainer.Enabled {
		t.Error("devcontainer should default enabled")
	}
}

func TestLoadProjectOverride(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "weft.yaml")
	content := `
branch:
  prefix: "wt/"
claude:
  command: "claude-code"
  exec_in_container: false
cleanup:
  require_clean: false
devcontainer:
  up_args: ["--build-no-cache"]
`
	if err := os.WriteFile(proj, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(Sources{ProjectPath: proj})
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch.Prefix != "wt/" {
		t.Errorf("prefix override failed: %q", c.Branch.Prefix)
	}
	if c.Claude.Command != "claude-code" || c.Claude.ExecInContainer {
		t.Errorf("claude override failed: %+v", c.Claude)
	}
	if c.Cleanup.RequireClean {
		t.Error("require_clean override failed")
	}
	// Unset fields keep their defaults.
	if c.Tmux.Session != "weft/{project}" {
		t.Errorf("unset field lost its default: %q", c.Tmux.Session)
	}
	if len(c.Devcontainer.UpArgs) != 1 || c.Devcontainer.UpArgs[0] != "--build-no-cache" {
		t.Errorf("up_args override failed: %+v", c.Devcontainer.UpArgs)
	}
}

func TestOverridePathReplacesProject(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "weft.yaml")
	override := filepath.Join(dir, "other.yaml")
	os.WriteFile(proj, []byte("branch:\n  prefix: \"proj/\"\n"), 0o644)
	os.WriteFile(override, []byte("branch:\n  prefix: \"override/\"\n"), 0o644)

	c, err := Load(Sources{ProjectPath: proj, OverridePath: override})
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch.Prefix != "override/" {
		t.Errorf("OverridePath should win, got %q", c.Branch.Prefix)
	}
}

func TestExpand(t *testing.T) {
	got := Expand("worktrees/{project}/{name}", map[string]string{"project": "app", "name": "feat"})
	if got != "worktrees/app/feat" {
		t.Errorf("Expand tokens = %q", got)
	}
	home := Expand("~/.weft/{project}", map[string]string{"project": "app"})
	if strings.HasPrefix(home, "~") {
		t.Errorf("tilde not expanded: %q", home)
	}
	if !strings.HasSuffix(home, "/.weft/app") {
		t.Errorf("unexpected expansion: %q", home)
	}
}

func TestClaudeProcessNames(t *testing.T) {
	c := Defaults()
	c.Claude.Command = "claude"
	names := c.ClaudeProcessNames()
	// Deduped: claude + node.
	if len(names) != 2 || names[0] != "claude" {
		t.Errorf("process names = %v", names)
	}
}
