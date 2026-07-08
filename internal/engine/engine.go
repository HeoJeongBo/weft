// Package engine is the orchestration facade shared by the CLI and the TUI. It
// composes the external-tool wrappers around a resolved project and config.
package engine

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/HeoJeongBo/weft/internal/config"
	"github.com/HeoJeongBo/weft/internal/devcontainer"
	"github.com/HeoJeongBo/weft/internal/dockerx"
	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/git"
	"github.com/HeoJeongBo/weft/internal/paths"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/tmux"
)

// Engine ties the wrappers, config, and resolved project together.
type Engine struct {
	Runner  sysexec.Runner
	Git     git.Git
	Tmux    tmux.Tmux
	Docker  dockerx.Docker
	DC      devcontainer.Devcontainer
	Cfg     config.Config
	Project domain.Project
	Log     *slog.Logger
}

// Open resolves the git repository containing cwd, loads configuration, and
// builds an Engine. overrideConfig, when non-empty, replaces project weft.yaml
// discovery.
func Open(ctx context.Context, r sysexec.Runner, log *slog.Logger, cwd, overrideConfig string) (*Engine, error) {
	root, err := git.Toplevel(ctx, r, cwd)
	if err != nil {
		return nil, err
	}
	g := git.New(r, root)

	cfg, err := config.Load(config.Sources{
		UserPath:     paths.UserConfig(),
		ProjectPath:  filepath.Join(root, "weft.yaml"),
		OverridePath: overrideConfig,
	})
	if err != nil {
		return nil, err
	}

	name := cfg.Project.Name
	if name == "" {
		name = filepath.Base(root)
	}
	slug := domain.Slugify(name)

	baseBranch := cfg.Project.BaseBranch
	if baseBranch == "" {
		baseBranch, _ = g.DefaultBranch(ctx)
	}

	vars := map[string]string{"project": slug, "repo": root}
	proj := domain.Project{
		Name:          name,
		Slug:          slug,
		Root:          root,
		DefaultBranch: baseBranch,
		ConfigPath:    cfg.Devcontainer.Config,
		TmuxSession:   config.Expand(cfg.Tmux.Session, vars),
	}

	return &Engine{
		Runner:  r,
		Git:     g,
		Tmux:    tmux.New(r),
		Docker:  dockerx.New(r),
		DC:      devcontainer.New(r),
		Cfg:     cfg,
		Project: proj,
		Log:     log,
	}, nil
}

// worktreePath returns the absolute worktree path for a session name.
func (e *Engine) worktreePath(name string) string {
	root := config.Expand(e.Cfg.Worktree.Root, map[string]string{
		"project": e.Project.Slug,
		"repo":    e.Project.Root,
		"name":    name,
	})
	return filepath.Join(root, name)
}

// branchName returns the git branch for a session name.
func (e *Engine) branchName(name string) string {
	return e.Cfg.Branch.Prefix + name
}

// sessionLabel returns the docker weft.session label value for a session name.
func (e *Engine) sessionLabel(name string) string {
	return domain.SessionKey(e.Project.Slug, name)
}
