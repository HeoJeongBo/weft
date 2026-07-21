package engine

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

// sagaEngine builds an Engine over a fake runner with a real repo root (holding a
// devcontainer.json) and a temp worktree root.
func sagaEngine(t *testing.T, f *sysexec.FakeRunner) *Engine {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".devcontainer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devcontainer", "devcontainer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Worktree.Root = filepath.Join(t.TempDir(), "{project}")
	return &Engine{
		Runner:  f,
		Git:     git.New(f, root),
		Tmux:    tmux.New(f),
		Docker:  dockerx.New(f),
		DC:      devcontainer.New(f),
		Cfg:     cfg,
		Project: domain.Project{Slug: "app", Root: root, DefaultBranch: "main", TmuxSession: "weft/app", ConfigPath: cfg.Devcontainer.Config},
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func cmdErr(c sysexec.Call, code int) error {
	return &wefterr.CmdError{Cmd: c.Name, Args: c.Args, ExitCode: code}
}

// sagaHandler answers the commands a `weft new` issues. upOK toggles success.
func sagaHandler(upOK bool, winID string) func(sysexec.Call) (sysexec.Result, error) {
	return func(c sysexec.Call) (sysexec.Result, error) {
		line := c.Line()
		switch {
		case strings.Contains(line, "worktree list"),
			strings.Contains(line, "docker ps"),
			strings.Contains(line, "list-windows"):
			return sysexec.Result{}, nil
		case strings.Contains(line, "rev-parse --verify"): // branch does not exist
			return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
		case strings.Contains(line, "has-session"): // tmux session absent
			return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
		case strings.Contains(line, "devcontainer up"):
			if upOK {
				return sysexec.Result{Stdout: `{"outcome":"success","containerId":"c-new","remoteUser":"vscode"}`}, nil
			}
			return sysexec.Result{Stdout: `{"outcome":"error","description":"image build failed"}`}, nil
		case strings.Contains(line, "new-window"):
			return sysexec.Result{Stdout: winID + "\n"}, nil
		default:
			return sysexec.Result{}, nil
		}
	}
}

func TestNewSagaSuccess(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: sagaHandler(true, "@5")}
	e := sagaEngine(t, f)

	var steps []string
	sink := func(ev Event) {
		if ev.Kind == EventStep {
			steps = append(steps, ev.Step)
		}
	}
	s, err := e.New(context.Background(), NewSpec{Name: "feat"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if s.Branch != "weft/feat" {
		t.Errorf("branch = %q", s.Branch)
	}
	if s.Window == nil || s.Window.ID != "@5" {
		t.Errorf("window = %+v", s.Window)
	}
	if s.Container == nil || s.Container.ID != "c-new" {
		t.Errorf("container = %+v", s.Container)
	}
	if s.Status != domain.StatusReady {
		t.Errorf("status = %s, want ready", s.Status)
	}
	joined := strings.Join(steps, ",")
	for _, want := range []string{"worktree", "devcontainer up", "tmux window"} {
		if !strings.Contains(joined, want) {
			t.Errorf("steps %q missing %q", joined, want)
		}
	}

	var upLine string
	for _, c := range f.Calls {
		if l := c.Line(); strings.Contains(l, "devcontainer up") {
			upLine = l
			break
		}
	}
	gitDir := filepath.Join(e.Project.Root, ".git")
	for _, want := range []string{
		"--config " + filepath.Join(e.Project.Root, ".devcontainer/devcontainer.json"),
		"--id-label weft.session=app/feat",
		"--id-label weft.project=app",
		"--id-label weft.branch=weft/feat",
		"--id-label weft.base_ref=main",
		"--id-label weft.created_at=",
		"--mount type=bind,source=" + gitDir + ",target=" + gitDir,
	} {
		if !strings.Contains(upLine, want) {
			t.Errorf("up argv %q missing %q", upLine, want)
		}
	}
}

func TestNewSagaRollsBackOnUpFailure(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: sagaHandler(false, "@5")}
	e := sagaEngine(t, f)

	_, err := e.New(context.Background(), NewSpec{Name: "feat"}, nil)
	if err == nil || !strings.Contains(err.Error(), "image build failed") {
		t.Fatalf("want up failure error, got %v", err)
	}

	// The worktree created before the failed `up` must have been removed.
	var removedWorktree, createdWindow bool
	for _, c := range f.Calls {
		line := c.Line()
		if strings.Contains(line, "worktree remove") {
			removedWorktree = true
		}
		if strings.Contains(line, "new-window") {
			createdWindow = true
		}
	}
	if !removedWorktree {
		t.Error("rollback did not remove the worktree")
	}
	if createdWindow {
		t.Error("should not have reached tmux new-window after up failure")
	}
}

func TestNewRejectsInvalidName(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: sagaHandler(true, "@5")}
	e := sagaEngine(t, f)
	if _, err := e.New(context.Background(), NewSpec{Name: "bad name"}, nil); err == nil {
		t.Fatal("want error for invalid name")
	}
}
