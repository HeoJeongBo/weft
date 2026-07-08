package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func rmHandler(dirty bool) func(sysexec.Call) (sysexec.Result, error) {
	return func(c sysexec.Call) (sysexec.Result, error) {
		line := c.Line()
		switch {
		case strings.Contains(line, "worktree list"):
			return sysexec.Result{Stdout: "worktree /repo\nHEAD a\nbranch refs/heads/main\n\n" +
				"worktree /wt/feat\nHEAD b\nbranch refs/heads/weft/feat\n"}, nil
		case strings.Contains(line, "docker ps"), strings.Contains(line, "list-windows"):
			return sysexec.Result{}, nil
		case strings.Contains(line, "status --porcelain"):
			if dirty {
				return sysexec.Result{Stdout: " M file.go\n"}, nil
			}
			return sysexec.Result{}, nil
		case strings.Contains(line, "rev-list"):
			return sysexec.Result{Stdout: "0\t0\n"}, nil
		default:
			return sysexec.Result{}, nil
		}
	}
}

func TestRemoveDirtyGuard(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: rmHandler(true)}
	e := sagaEngine(t, f)
	err := e.Remove(context.Background(), RemoveSpec{Name: "feat"}, nil)
	if !errors.Is(err, wefterr.ErrDirtyWorktree) {
		t.Fatalf("want ErrDirtyWorktree, got %v", err)
	}
	// Nothing should have been torn down.
	for _, c := range f.Calls {
		if strings.Contains(c.Line(), "worktree remove") {
			t.Error("dirty guard should prevent worktree removal")
		}
	}
}

func TestRemoveForce(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: rmHandler(true)}
	e := sagaEngine(t, f)
	if err := e.Remove(context.Background(), RemoveSpec{Name: "feat", Force: true}, nil); err != nil {
		t.Fatal(err)
	}
	var forcedRemove bool
	for _, c := range f.Calls {
		if strings.Contains(c.Line(), "worktree remove") && strings.Contains(c.Line(), "--force") {
			forcedRemove = true
		}
	}
	if !forcedRemove {
		t.Error("want forced worktree remove")
	}
}

func TestRemoveMissingSession(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: rmHandler(false)}
	e := sagaEngine(t, f)
	err := e.Remove(context.Background(), RemoveSpec{Name: "ghost"}, nil)
	if !errors.Is(err, wefterr.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}
