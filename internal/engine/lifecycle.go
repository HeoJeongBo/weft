package engine

import (
	"context"
	"fmt"

	"github.com/HeoJeongBo/weft/internal/devcontainer"
	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// Stop pauses a session by stopping its container. The worktree, branch, and
// tmux window are left intact so it can be resumed with Start.
func (e *Engine) Stop(ctx context.Context, name string, sink Sink) error {
	s, ok, err := e.FindSession(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", wefterr.ErrSessionNotFound, name)
	}
	if s.Container == nil {
		sink.log("no container to stop")
		return nil
	}
	sink.step("stop container")
	if err := e.Docker.Stop(ctx, s.Container.ID); err != nil {
		sink.fail(err)
		return err
	}
	sink.done(s)
	return nil
}

// Start resumes a stopped session: it brings the container back up and ensures a
// tmux window running claude exists.
func (e *Engine) Start(ctx context.Context, name string, noClaude bool, sink Sink) error {
	s, ok, err := e.FindSession(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", wefterr.ErrSessionNotFound, name)
	}
	if s.Worktree == nil {
		return fmt.Errorf("session %s has no worktree; recreate it with `weft new %s`", name, name)
	}
	wtPath := s.Worktree.Path
	dcEnabled := e.Cfg.Devcontainer.Enabled

	if dcEnabled {
		sink.step("devcontainer up")
		up, err := e.DC.Up(ctx, func(l sysexec.Line) { sink.log(l.Text) }, devcontainer.UpOpts{
			WorkspaceFolder: wtPath,
			ConfigPath:      e.dcConfigPath(),
			IDLabels:        e.idLabels(name),
			ExtraArgs:       e.upExtraArgs(),
		})
		if err != nil {
			sink.fail(err)
			return err
		}
		e.setupGitSafe(ctx, name, wtPath, up.RemoteWorkspaceFolder, sink)
	}

	if s.Window == nil {
		sink.step("tmux window")
		if err := e.ensureTmuxSession(ctx); err != nil {
			sink.fail(err)
			return err
		}
		launch := e.launchCommand(NewSpec{Name: name, NoClaude: noClaude}, wtPath, dcEnabled)
		if _, err := e.Tmux.NewWindow(ctx, e.Project.TmuxSession, name, wtPath, launch); err != nil {
			sink.fail(err)
			return err
		}
	}
	sink.done(s)
	return nil
}

// Exec runs a command inside a session's container (or on the host in the
// worktree when devcontainer is disabled) and returns the captured result.
func (e *Engine) Exec(ctx context.Context, name string, cmd ...string) (sysexec.Result, error) {
	s, ok, err := e.FindSession(ctx, name)
	if err != nil {
		return sysexec.Result{}, err
	}
	if !ok {
		return sysexec.Result{}, fmt.Errorf("%w: %s", wefterr.ErrSessionNotFound, name)
	}
	if s.Worktree == nil {
		return sysexec.Result{}, fmt.Errorf("session %s has no worktree", name)
	}
	if s.Container != nil {
		return e.DC.Exec(ctx, e.execOpts(name, s.Worktree.Path), cmd...)
	}
	// Host fallback.
	script := "cd " + singleQuote(s.Worktree.Path) + " && " + shellJoin(cmd)
	return e.Runner.Run(ctx, "sh", "-c", script)
}

// RepairReport summarizes what Repair cleaned up.
type RepairReport struct {
	PrunedWorktrees  bool
	OrphanContainers int
	OrphanWindows    int
}

// Repair reconciles and cleans up orphans: prunes stale git worktree metadata and
// removes containers/windows whose worktree is gone.
func (e *Engine) Repair(ctx context.Context, sink Sink) (RepairReport, error) {
	sessions, err := e.Reconcile(ctx)
	if err != nil {
		return RepairReport{}, err
	}
	var rep RepairReport

	sink.step("prune worktrees")
	if err := e.Git.Prune(ctx); err == nil {
		rep.PrunedWorktrees = true
	}

	for _, s := range sessions {
		if s.Status != domain.StatusOrphaned {
			continue
		}
		if s.Container != nil {
			sink.step("remove orphan container: " + s.Name)
			if n, err := e.Docker.RemoveByLabel(ctx, "weft.session="+e.sessionLabel(s.Name), true); err == nil {
				rep.OrphanContainers += n
			}
		}
		if s.Window != nil {
			sink.step("kill orphan window: " + s.Name)
			if err := e.Tmux.KillWindow(ctx, e.Project.WindowTarget(s.Name)); err == nil {
				rep.OrphanWindows++
			}
		}
	}
	return rep, nil
}

func shellJoin(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += singleQuote(a)
	}
	return out
}
