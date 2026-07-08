package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// RemoveSpec describes a teardown.
type RemoveSpec struct {
	Name         string
	Force        bool // bypass the dirty/unpushed guard and force worktree/branch removal
	DeleteBranch bool // delete the branch (otherwise kept unless config says delete)
	KeepBranch   bool // keep the branch even if config/DeleteBranch would remove it
}

// Remove tears a session down: tmux window, container, worktree, and optionally
// branch — guarding against destroying uncommitted or unpushed work. Teardown is
// best-effort; partial failures are aggregated and the operation is re-runnable.
func (e *Engine) Remove(ctx context.Context, spec RemoveSpec, sink Sink) error {
	s, ok, err := e.FindSession(ctx, spec.Name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", wefterr.ErrSessionNotFound, spec.Name)
	}

	// ---- Data-loss guard ----
	if e.Cfg.Cleanup.RequireClean && !spec.Force && s.Worktree != nil {
		if s.Worktree.Dirty {
			return fmt.Errorf("%w: %s has uncommitted changes (use --force to discard)", wefterr.ErrDirtyWorktree, spec.Name)
		}
		if s.Worktree.Ahead > 0 {
			return fmt.Errorf("%w: %s has %d unpushed commit(s) (use --force)", wefterr.ErrDirtyWorktree, spec.Name, s.Worktree.Ahead)
		}
	}

	for _, h := range e.Cfg.Hooks.PreRemove {
		sink.step("hook: " + h)
		if err := e.runHook(ctx, worktreePathOf(s), s.Container != nil, h); err != nil {
			sink.log(fmt.Sprintf("pre_remove hook failed (continuing): %v", err))
		}
	}

	var errs []error

	// 1. tmux window.
	if s.Window != nil {
		sink.step("kill window")
		if err := e.Tmux.KillWindow(ctx, e.Project.WindowTarget(spec.Name)); err != nil {
			errs = append(errs, fmt.Errorf("kill window: %w", err))
		}
	}

	// 2. container (by label; devcontainer has no "down").
	sink.step("remove container")
	if _, err := e.Docker.RemoveByLabel(ctx, "weft.session="+e.sessionLabel(spec.Name), true); err != nil {
		errs = append(errs, fmt.Errorf("remove container: %w", err))
	}

	// 3. worktree.
	if s.Worktree != nil {
		sink.step("remove worktree")
		if err := e.Git.RemoveWorktree(ctx, s.Worktree.Path, spec.Force); err != nil {
			errs = append(errs, fmt.Errorf("remove worktree: %w", err))
		}
		_ = e.Git.Prune(ctx)
	}

	// 4. branch (kept by default — it is the user's work product).
	deleteBranch := (e.Cfg.Cleanup.DeleteBranch || spec.DeleteBranch) && !spec.KeepBranch
	if deleteBranch && s.Branch != "" {
		sink.step("delete branch")
		if err := e.Git.DeleteBranch(ctx, s.Branch, spec.Force); err != nil {
			errs = append(errs, fmt.Errorf("delete branch: %w", err))
		}
	}

	for _, h := range e.Cfg.Hooks.PostRemove {
		sink.step("hook: " + h)
		if err := e.runHook(ctx, e.Project.Root, false, h); err != nil {
			sink.log(fmt.Sprintf("post_remove hook failed (continuing): %v", err))
		}
	}

	if len(errs) > 0 {
		err := errors.Join(errs...)
		sink.fail(err)
		return err
	}
	sink.done(domain.Session{Name: spec.Name, Project: e.Project.Slug})
	return nil
}

func worktreePathOf(s domain.Session) string {
	if s.Worktree != nil {
		return s.Worktree.Path
	}
	return ""
}
