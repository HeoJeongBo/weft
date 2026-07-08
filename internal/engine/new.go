package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HeoJeongBo/weft/internal/devcontainer"
	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/paths"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// NewSpec describes a session to create. Empty fields fall back to config.
type NewSpec struct {
	Name           string
	BaseRef        string // base ref to branch from; "" = project default branch
	Branch         string // branch name; "" = prefix + name
	NoDevcontainer bool
	NoClaude       bool
	Force          bool // reuse an existing branch
	KeepOnFailure  bool // don't roll back on failure
}

// rollbackFn undoes one mutation. Rollbacks run with a fresh context so a
// cancelled operation can still clean up.
type rollbackFn func(context.Context)

// New creates a session: worktree + branch, devcontainer up, tmux window running
// claude — as one motion, rolling back on failure. Progress is reported to sink.
func (e *Engine) New(ctx context.Context, spec NewSpec, rawSink Sink) (domain.Session, error) {
	sink := rawSink

	// ---- Preflight (no mutations) ----
	if !domain.ValidName(spec.Name) {
		return domain.Session{}, fmt.Errorf("invalid session name %q (use letters, digits, . _ -)", spec.Name)
	}
	if existing, ok, err := e.FindSession(ctx, spec.Name); err != nil {
		return domain.Session{}, err
	} else if ok {
		sink.fail(fmt.Errorf("%w: %s", wefterr.ErrSessionExists, spec.Name))
		return existing, fmt.Errorf("%w: %s", wefterr.ErrSessionExists, spec.Name)
	}

	branch := spec.Branch
	if branch == "" {
		branch = e.branchName(spec.Name)
	}
	base := spec.BaseRef
	if base == "" {
		base = e.Project.DefaultBranch
	}
	wtPath := e.worktreePath(spec.Name)
	dcEnabled := e.Cfg.Devcontainer.Enabled && !spec.NoDevcontainer

	if dcEnabled {
		cfgFile := filepath.Join(e.Project.Root, e.Cfg.Devcontainer.Config)
		if !paths.Exists(cfgFile) {
			err := fmt.Errorf("%w at %s (use --no-devcontainer)", wefterr.ErrDevcontainerMissing, e.Cfg.Devcontainer.Config)
			sink.fail(err)
			return domain.Session{}, err
		}
	}

	branchExists, err := e.Git.BranchExists(ctx, branch)
	if err != nil {
		return domain.Session{}, err
	}
	if branchExists && !spec.Force {
		err := fmt.Errorf("branch %s already exists (use --force to reuse it or pick another name)", branch)
		sink.fail(err)
		return domain.Session{}, err
	}
	createBranch := !branchExists

	// ---- Mutations (each pushes a rollback) ----
	var rollbacks []rollbackFn
	failed := func(err error) (domain.Session, error) {
		if !spec.KeepOnFailure && len(rollbacks) > 0 {
			sink.log("rolling back…")
			rbCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			for i := len(rollbacks) - 1; i >= 0; i-- {
				rollbacks[i](rbCtx)
			}
			cancel()
		}
		sink.fail(err)
		return domain.Session{}, err
	}

	// 1. Worktree + branch.
	sink.step("worktree")
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return failed(fmt.Errorf("create worktree parent: %w", err))
	}
	if err := e.Git.AddWorktree(ctx, wtPath, branch, base, createBranch); err != nil {
		return failed(fmt.Errorf("git worktree add: %w", err))
	}
	rollbacks = append(rollbacks, func(c context.Context) {
		_ = e.Git.RemoveWorktree(c, wtPath, true)
		if createBranch {
			_ = e.Git.DeleteBranch(c, branch, true)
		}
	})

	// 2. Devcontainer.
	var container *domain.Container
	if dcEnabled {
		sink.step("devcontainer up")
		label := e.sessionLabel(spec.Name)
		up, err := e.DC.Up(ctx, func(l sysexec.Line) { sink.log(l.Text) }, devcontainer.UpOpts{
			WorkspaceFolder: wtPath,
			ConfigPath:      e.Cfg.Devcontainer.Config,
			IDLabels:        []string{"weft.session=" + label, "weft.project=" + e.Project.Slug},
			ExtraArgs:       e.Cfg.Devcontainer.UpArgs,
		})
		if err != nil {
			return failed(err)
		}
		container = &domain.Container{
			ID:                    up.ContainerID,
			State:                 "running",
			RemoteUser:            up.RemoteUser,
			RemoteWorkspaceFolder: up.RemoteWorkspaceFolder,
		}
		rollbacks = append(rollbacks, func(c context.Context) {
			_, _ = e.Docker.RemoveByLabel(c, "weft.session="+label, true)
		})
	}

	// 3. post_create hooks.
	for _, h := range e.Cfg.Hooks.PostCreate {
		sink.step("hook: " + h)
		if err := e.runHook(ctx, wtPath, dcEnabled, h); err != nil {
			return failed(fmt.Errorf("post_create hook %q: %w", h, err))
		}
	}

	// 4. tmux window running claude.
	sink.step("tmux window")
	if err := e.ensureTmuxSession(ctx); err != nil {
		return failed(fmt.Errorf("tmux session: %w", err))
	}
	launch := e.launchCommand(spec, wtPath, dcEnabled)
	winID, err := e.Tmux.NewWindow(ctx, e.Project.TmuxSession, spec.Name, wtPath, launch)
	if err != nil {
		return failed(fmt.Errorf("tmux new-window: %w", err))
	}
	rollbacks = append(rollbacks, func(c context.Context) {
		_ = e.Tmux.KillWindow(c, winID)
	})

	// ---- Success ----
	s := domain.Session{
		Name:      spec.Name,
		Project:   e.Project.Slug,
		Branch:    branch,
		BaseRef:   base,
		CreatedAt: time.Now(),
		Worktree:  &domain.Worktree{Path: wtPath, Branch: branch},
		Container: container,
		Window:    &domain.Window{ID: winID, Name: spec.Name},
	}
	s.DeriveStatus(e.Cfg.ClaudeProcessNames()...)
	sink.done(s)
	return s, nil
}

// launchCommand builds the tmux window's foreground command.
func (e *Engine) launchCommand(spec NewSpec, wtPath string, dcEnabled bool) []string {
	if spec.NoClaude {
		return nil // default shell in the worktree
	}
	claudeCmd := append([]string{e.Cfg.Claude.Command}, e.Cfg.Claude.Args...)
	if dcEnabled && e.Cfg.Claude.ExecInContainer {
		return devcontainer.ExecArgs(devcontainer.ExecOpts{
			WorkspaceFolder: wtPath,
			ConfigPath:      e.Cfg.Devcontainer.Config,
		}, claudeCmd...)
	}
	return claudeCmd
}

// ensureTmuxSession creates the project's tmux session if it does not exist.
func (e *Engine) ensureTmuxSession(ctx context.Context) error {
	has, err := e.Tmux.HasSession(ctx, e.Project.TmuxSession)
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	return e.Tmux.NewSession(ctx, e.Project.TmuxSession, e.Project.Root)
}

// runHook runs a hook command, in the container when devcontainer is enabled,
// otherwise on the host in the worktree.
func (e *Engine) runHook(ctx context.Context, wtPath string, inContainer bool, hook string) error {
	if inContainer {
		_, err := e.DC.Exec(ctx, devcontainer.ExecOpts{
			WorkspaceFolder: wtPath,
			ConfigPath:      e.Cfg.Devcontainer.Config,
		}, "sh", "-lc", hook)
		return err
	}
	_, err := e.Runner.Mutate(ctx, "sh", "-c", "cd "+singleQuote(wtPath)+" && "+hook)
	return err
}

func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
