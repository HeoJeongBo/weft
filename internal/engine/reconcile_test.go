package engine

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/config"
	"github.com/HeoJeongBo/weft/internal/devcontainer"
	"github.com/HeoJeongBo/weft/internal/dockerx"
	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/git"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/tmux"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

const (
	worktreePorcelain = `worktree /repo
HEAD aaaa
branch refs/heads/main

worktree /home/u/.weft/worktrees/app/feat-auth
HEAD bbbb
branch refs/heads/weft/feat-auth

worktree /home/u/.weft/worktrees/app/spike
HEAD cccc
branch refs/heads/weft/spike
`
	// feat-auth container (running) + an orphan (ghost) with no worktree.
	dockerPs = `{"ID":"c1","Image":"go:1.24","State":"running","Labels":"weft.project=app,weft.session=app/feat-auth"}
{"ID":"c2","Image":"go:1.24","State":"exited","Labels":"weft.project=app,weft.session=app/ghost"}`
	// feat-auth window running claude, plus a stray default window that must be ignored.
	tmuxWindows = "@1\tfeat-auth\t1\t1\tclaude\t0\t1720000000\n@0\t0\t0\t0\tbash\t0\t1720000000\n"
)

func testEngine(handler func(sysexec.Call) (sysexec.Result, error)) *Engine {
	f := &sysexec.FakeRunner{Handler: handler}
	return &Engine{
		Runner:  f,
		Git:     git.New(f, "/repo"),
		Tmux:    tmux.New(f),
		Docker:  dockerx.New(f),
		DC:      devcontainer.New(f),
		Cfg:     config.Defaults(),
		Project: domain.Project{Slug: "app", Root: "/repo", DefaultBranch: "main", TmuxSession: "weft/app"},
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func reconcileHandler(c sysexec.Call) (sysexec.Result, error) {
	line := c.Line()
	switch {
	case strings.Contains(line, "worktree list"):
		return sysexec.Result{Stdout: worktreePorcelain}, nil
	case strings.Contains(line, "docker ps"):
		return sysexec.Result{Stdout: dockerPs}, nil
	case strings.Contains(line, "list-windows"):
		return sysexec.Result{Stdout: tmuxWindows}, nil
	default:
		return sysexec.Result{}, nil
	}
}

func TestReconcileJoin(t *testing.T) {
	e := testEngine(reconcileHandler)
	sessions, err := e.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// feat-auth, ghost, spike — sorted; the stray "0" tmux window is NOT a session.
	if len(sessions) != 3 {
		t.Fatalf("want 3 sessions, got %d: %+v", len(sessions), names(sessions))
	}
	got := map[string]domain.Session{}
	for _, s := range sessions {
		got[s.Name] = s
	}

	fa := got["feat-auth"]
	if fa.Status != domain.StatusReady || fa.Claude != domain.ClaudeRunning {
		t.Errorf("feat-auth: status=%s claude=%s, want ready/running", fa.Status, fa.Claude)
	}
	if fa.Worktree == nil || fa.Container == nil || fa.Window == nil {
		t.Errorf("feat-auth should have all three pieces: %+v", fa)
	}

	if got["spike"].Status != domain.StatusPartial {
		t.Errorf("spike status = %s, want partial", got["spike"].Status)
	}
	if got["ghost"].Status != domain.StatusOrphaned {
		t.Errorf("ghost status = %s, want orphaned", got["ghost"].Status)
	}
	if got["ghost"].Worktree != nil {
		t.Errorf("ghost should have no worktree")
	}
}

func TestReconcileDegradesWhenDockerDown(t *testing.T) {
	// Docker errors; reconcile should still return worktree-derived sessions.
	e := testEngine(func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), "docker") {
			return sysexec.Result{ExitCode: 1}, &wefterr.CmdError{Cmd: "docker", Args: c.Args, ExitCode: 1}
		}
		return reconcileHandler(c)
	})
	sessions, err := e.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// ghost came only from docker, so without docker we get feat-auth + spike.
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions without docker, got %d: %v", len(sessions), names(sessions))
	}
}

func TestEnginePathHelpers(t *testing.T) {
	e := testEngine(nil)
	if got := e.branchName("feat-auth"); got != "weft/feat-auth" {
		t.Errorf("branchName = %q", got)
	}
	if got := e.sessionLabel("feat-auth"); got != "app/feat-auth" {
		t.Errorf("sessionLabel = %q", got)
	}
	wt := e.worktreePath("feat-auth")
	if !strings.HasSuffix(wt, "/.weft/worktrees/app/feat-auth") {
		t.Errorf("worktreePath = %q", wt)
	}
}

func names(ss []domain.Session) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}
