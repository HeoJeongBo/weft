// Package config defines weft's configuration schema, defaults, layered loading,
// and token expansion.
package config

import (
	"strings"

	"github.com/HeoJeongBo/weft/internal/paths"
)

// Config is the fully-resolved weft configuration.
type Config struct {
	Version      int             `koanf:"version"`
	Project      ProjectCfg      `koanf:"project"`
	Branch       BranchCfg       `koanf:"branch"`
	Worktree     WorktreeCfg     `koanf:"worktree"`
	Devcontainer DevcontainerCfg `koanf:"devcontainer"`
	Tmux         TmuxCfg         `koanf:"tmux"`
	Claude       ClaudeCfg       `koanf:"claude"`
	Hooks        HooksCfg        `koanf:"hooks"`
	Cleanup      CleanupCfg      `koanf:"cleanup"`
}

// ProjectCfg identifies the project and its base branch.
type ProjectCfg struct {
	Name       string `koanf:"name"`
	BaseBranch string `koanf:"base_branch"`
}

// BranchCfg configures session branch naming.
type BranchCfg struct {
	Prefix string `koanf:"prefix"`
}

// WorktreeCfg configures where session worktrees live.
type WorktreeCfg struct {
	Root string `koanf:"root"`
}

// DevcontainerCfg configures devcontainer usage.
type DevcontainerCfg struct {
	Enabled  bool     `koanf:"enabled"`
	Config   string   `koanf:"config"`
	MountGit bool     `koanf:"mount_git"`
	UpArgs   []string `koanf:"up_args"`
}

// TmuxCfg configures tmux session/window naming.
type TmuxCfg struct {
	Session string `koanf:"session"`
	Window  string `koanf:"window"`
}

// ClaudeCfg configures how claude is launched in a session.
type ClaudeCfg struct {
	Command         string   `koanf:"command"`
	ExecInContainer bool     `koanf:"exec_in_container"`
	Args            []string `koanf:"args"`
}

// HooksCfg holds lifecycle hook commands.
type HooksCfg struct {
	PostCreate []string `koanf:"post_create"`
	PreRemove  []string `koanf:"pre_remove"`
	PostRemove []string `koanf:"post_remove"`
}

// CleanupCfg configures teardown behavior.
type CleanupCfg struct {
	DeleteBranch bool `koanf:"delete_branch"`
	RequireClean bool `koanf:"require_clean"`
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		Version: 1,
		Branch:  BranchCfg{Prefix: "weft/"},
		Worktree: WorktreeCfg{
			Root: "~/.weft/worktrees/{project}",
		},
		Devcontainer: DevcontainerCfg{
			Enabled:  true,
			Config:   ".devcontainer/devcontainer.json",
			MountGit: true,
		},
		Tmux: TmuxCfg{
			Session: "weft/{project}",
			Window:  "{name}",
		},
		Claude: ClaudeCfg{
			Command:         "claude",
			ExecInContainer: true,
		},
		Cleanup: CleanupCfg{
			RequireClean: true,
		},
	}
}

// Expand substitutes {key} tokens from vars and expands a leading ~.
func Expand(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return paths.ExpandTilde(out)
}

// ClaudeProcessNames returns the process names that count as a live claude in a
// tmux pane (the configured command plus common launchers).
func (c Config) ClaudeProcessNames() []string {
	names := []string{c.Claude.Command, "claude", "node"}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
