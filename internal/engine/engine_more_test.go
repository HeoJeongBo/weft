package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// discardLog is a logger that throws everything away.
func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// override wraps base so that fn (matched on the command line) can shadow
// specific commands. fn returns ok=false to fall through to base.
func override(base func(sysexec.Call) (sysexec.Result, error), fn func(line string) (sysexec.Result, error, bool)) func(sysexec.Call) (sysexec.Result, error) {
	return func(c sysexec.Call) (sysexec.Result, error) {
		if r, e, ok := fn(c.Line()); ok {
			return r, e
		}
		return base(c)
	}
}

// upSuccessJSON is a devcontainer up success payload.
const upSuccessJSON = `{"outcome":"success","containerId":"c-new","remoteUser":"vscode"}`

// -------------------- Open --------------------

func TestOpen(t *testing.T) {
	// Toplevel error -> Open fails.
	t.Run("toplevel error", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			return sysexec.Result{ExitCode: 128}, cmdErr(c, 128)
		}}
		if _, err := Open(context.Background(), f, discardLog(), "/nowhere", ""); !errors.Is(err, wefterr.ErrNotInRepo) {
			t.Fatalf("want ErrNotInRepo, got %v", err)
		}
	})

	// Success, no weft.yaml -> name defaults to base(root), DefaultBranch from origin/HEAD.
	t.Run("defaults from symbolic-ref", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		root := t.TempDir()
		f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			switch {
			case strings.Contains(line, "rev-parse --show-toplevel"):
				return sysexec.Result{Stdout: root + "\n"}, nil
			case strings.Contains(line, "symbolic-ref"):
				return sysexec.Result{Stdout: "origin/main\n"}, nil
			default:
				return sysexec.Result{}, nil
			}
		}}
		e, err := Open(context.Background(), f, discardLog(), root, "")
		if err != nil {
			t.Fatal(err)
		}
		if e.Project.Name != filepath.Base(root) {
			t.Errorf("name = %q, want %q", e.Project.Name, filepath.Base(root))
		}
		if e.Project.DefaultBranch != "main" {
			t.Errorf("DefaultBranch = %q, want main", e.Project.DefaultBranch)
		}
		if e.Project.TmuxSession != "weft/"+domain.Slugify(filepath.Base(root)) {
			t.Errorf("TmuxSession = %q", e.Project.TmuxSession)
		}
	})

	// symbolic-ref empty -> config init.defaultBranch fallback.
	t.Run("defaults from init.defaultBranch", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		root := t.TempDir()
		f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			switch {
			case strings.Contains(line, "rev-parse --show-toplevel"):
				return sysexec.Result{Stdout: root}, nil
			case strings.Contains(line, "symbolic-ref"):
				return sysexec.Result{Stdout: "\n"}, nil // empty -> skip
			case strings.Contains(line, "config --get init.defaultBranch"):
				return sysexec.Result{Stdout: "trunk\n"}, nil
			default:
				return sysexec.Result{}, nil
			}
		}}
		e, err := Open(context.Background(), f, discardLog(), root, "")
		if err != nil {
			t.Fatal(err)
		}
		if e.Project.DefaultBranch != "trunk" {
			t.Errorf("DefaultBranch = %q, want trunk", e.Project.DefaultBranch)
		}
	})

	// Both git sources empty -> "main" ultimate fallback.
	t.Run("defaults to main", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		root := t.TempDir()
		f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "rev-parse --show-toplevel") {
				return sysexec.Result{Stdout: root}, nil
			}
			return sysexec.Result{}, nil // symbolic-ref & config both empty
		}}
		e, err := Open(context.Background(), f, discardLog(), root, "")
		if err != nil {
			t.Fatal(err)
		}
		if e.Project.DefaultBranch != "main" {
			t.Errorf("DefaultBranch = %q, want main", e.Project.DefaultBranch)
		}
	})

	// weft.yaml with project.base_branch + name set -> DefaultBranch not resolved via git.
	t.Run("weft.yaml base_branch skips DefaultBranch", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		root := t.TempDir()
		yaml := "project:\n  name: My Proj\n  base_branch: develop\n"
		if err := os.WriteFile(filepath.Join(root, "weft.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "rev-parse --show-toplevel") {
				return sysexec.Result{Stdout: root}, nil
			}
			return sysexec.Result{}, nil
		}}
		e, err := Open(context.Background(), f, discardLog(), root, "")
		if err != nil {
			t.Fatal(err)
		}
		if e.Project.Name != "My Proj" {
			t.Errorf("name = %q", e.Project.Name)
		}
		if e.Project.Slug != "my-proj" {
			t.Errorf("slug = %q", e.Project.Slug)
		}
		if e.Project.DefaultBranch != "develop" {
			t.Errorf("DefaultBranch = %q, want develop", e.Project.DefaultBranch)
		}
		for _, c := range f.Calls {
			if strings.Contains(c.Line(), "symbolic-ref") {
				t.Error("DefaultBranch should not have been resolved via git")
			}
		}
	})

	// Malformed weft.yaml -> config.Load error.
	t.Run("malformed weft.yaml", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "weft.yaml"), []byte("project: [this is: not valid"), 0o644); err != nil {
			t.Fatal(err)
		}
		f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "rev-parse --show-toplevel") {
				return sysexec.Result{Stdout: root}, nil
			}
			return sysexec.Result{}, nil
		}}
		if _, err := Open(context.Background(), f, discardLog(), root, ""); err == nil {
			t.Fatal("want config load error")
		}
	})
}

// -------------------- CreateSession --------------------

func TestCreateSession(t *testing.T) {
	t.Run("success emits done", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: sagaHandler(true, "@5")}
		e := sagaEngine(t, f)
		var kinds []EventKind
		for ev := range e.CreateSession(context.Background(), NewSpec{Name: "feat"}) {
			kinds = append(kinds, ev.Kind)
		}
		if !containsKind(kinds, EventDone) {
			t.Errorf("want an EventDone, got %v", kinds)
		}
		if containsKind(kinds, EventError) {
			t.Errorf("unexpected EventError: %v", kinds)
		}
	})

	t.Run("failure emits error", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: sagaHandler(false, "@5")}
		e := sagaEngine(t, f)
		var kinds []EventKind
		for ev := range e.CreateSession(context.Background(), NewSpec{Name: "feat"}) {
			kinds = append(kinds, ev.Kind)
		}
		if !containsKind(kinds, EventError) {
			t.Errorf("want an EventError, got %v", kinds)
		}
		if containsKind(kinds, EventDone) {
			t.Errorf("unexpected EventDone: %v", kinds)
		}
	})
}

func containsKind(kinds []EventKind, want EventKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

// -------------------- Stop --------------------

func TestStop(t *testing.T) {
	t.Run("running container stopped", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		var kinds []EventKind
		err := e.Stop(context.Background(), "feat-auth", func(ev Event) { kinds = append(kinds, ev.Kind) })
		if err != nil {
			t.Fatal(err)
		}
		if !containsKind(kinds, EventDone) {
			t.Errorf("want EventDone, got %v", kinds)
		}
		if !calledLike(e, "docker stop c1") {
			t.Error("expected docker stop c1")
		}
	})

	t.Run("stop error fails", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "docker stop") {
				return sysexec.Result{ExitCode: 1}, errors.New("boom"), true
			}
			return sysexec.Result{}, nil, false
		})
		e := testEngine(h)
		var kinds []EventKind
		err := e.Stop(context.Background(), "feat-auth", func(ev Event) { kinds = append(kinds, ev.Kind) })
		if err == nil {
			t.Fatal("want stop error")
		}
		if !containsKind(kinds, EventError) {
			t.Errorf("want EventError, got %v", kinds)
		}
	})

	t.Run("no container is a no-op", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		var texts []string
		err := e.Stop(context.Background(), "spike", func(ev Event) {
			if ev.Kind == EventLog {
				texts = append(texts, ev.Text)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(texts, " ") == "" {
			t.Error("expected a log line about no container")
		}
	})

	t.Run("not found", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if err := e.Stop(context.Background(), "nope", nil); !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("reconcile error", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "worktree list") {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return reconcileHandler(c)
		}
		e := testEngine(h)
		if err := e.Stop(context.Background(), "feat-auth", nil); err == nil {
			t.Fatal("want reconcile error")
		}
	})
}

// -------------------- Start --------------------

func TestStart(t *testing.T) {
	// spike: worktree only. dcEnabled -> DC.Up; window nil -> ensureTmuxSession + NewWindow.
	t.Run("brings up container and window", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			switch {
			case strings.Contains(line, "devcontainer up"):
				return sysexec.Result{Stdout: upSuccessJSON}, nil, true
			case strings.Contains(line, "has-session"):
				return sysexec.Result{ExitCode: 1}, cmdErr(sysexec.Call{Name: "tmux"}, 1), true
			case strings.Contains(line, "new-window"):
				return sysexec.Result{Stdout: "@9\n"}, nil, true
			}
			return sysexec.Result{}, nil, false
		})
		e := testEngine(h)
		var kinds []EventKind
		if err := e.Start(context.Background(), "spike", false, func(ev Event) { kinds = append(kinds, ev.Kind) }); err != nil {
			t.Fatal(err)
		}
		if !containsKind(kinds, EventDone) {
			t.Errorf("want EventDone, got %v", kinds)
		}
		if !calledLike(e, "new-window") {
			t.Error("expected a tmux new-window")
		}
	})

	// feat-auth already has a window -> the window branch is skipped.
	t.Run("existing window skipped", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "devcontainer up") {
				return sysexec.Result{Stdout: upSuccessJSON}, nil, true
			}
			return sysexec.Result{}, nil, false
		})
		e := testEngine(h)
		if err := e.Start(context.Background(), "feat-auth", false, nil); err != nil {
			t.Fatal(err)
		}
		if calledLike(e, "new-window") {
			t.Error("should not create a window when one exists")
		}
	})

	t.Run("no worktree errors", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if err := e.Start(context.Background(), "ghost", false, nil); err == nil || !strings.Contains(err.Error(), "no worktree") {
			t.Fatalf("want no-worktree error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if err := e.Start(context.Background(), "nope", false, nil); !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("reconcile error", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "worktree list") {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return reconcileHandler(c)
		}
		e := testEngine(h)
		if err := e.Start(context.Background(), "spike", false, nil); err == nil {
			t.Fatal("want reconcile error")
		}
	})

	// spike has no window -> new-window is reached; make it fail.
	t.Run("new-window failure", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			switch {
			case strings.Contains(line, "devcontainer up"):
				return sysexec.Result{Stdout: upSuccessJSON}, nil, true
			case strings.Contains(line, "has-session"):
				return sysexec.Result{ExitCode: 1}, cmdErr(sysexec.Call{Name: "tmux"}, 1), true
			case strings.Contains(line, "tmux new-window"):
				return sysexec.Result{ExitCode: 1}, errors.New("nw fail"), true
			}
			return sysexec.Result{}, nil, false
		})
		e := testEngine(h)
		var kinds []EventKind
		if err := e.Start(context.Background(), "spike", false, func(ev Event) { kinds = append(kinds, ev.Kind) }); err == nil {
			t.Fatal("want new-window failure")
		}
		if !containsKind(kinds, EventError) {
			t.Errorf("want EventError, got %v", kinds)
		}
	})

	// noClaude -> launchCommand returns nil (default shell) on a successful start.
	t.Run("noClaude default shell", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			switch {
			case strings.Contains(line, "devcontainer up"):
				return sysexec.Result{Stdout: upSuccessJSON}, nil, true
			case strings.Contains(line, "has-session"):
				return sysexec.Result{ExitCode: 1}, cmdErr(sysexec.Call{Name: "tmux"}, 1), true
			case strings.Contains(line, "tmux new-window"):
				return sysexec.Result{Stdout: "@9\n"}, nil, true
			}
			return sysexec.Result{}, nil, false
		})
		e := testEngine(h)
		if err := e.Start(context.Background(), "spike", true, nil); err != nil {
			t.Fatal(err)
		}
		// launched with no foreground command (no trailing "--").
		for _, c := range e.Runner.(*sysexec.FakeRunner).Calls {
			if strings.Contains(c.Line(), "tmux new-window") && strings.Contains(c.Line(), " -- ") {
				t.Errorf("noClaude should launch a default shell (no --): %q", c.Line())
			}
		}
	})

	t.Run("devcontainer up failure", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "devcontainer up") {
				return sysexec.Result{Stdout: `{"outcome":"error","description":"build failed"}`}, nil, true
			}
			return sysexec.Result{}, nil, false
		})
		e := testEngine(h)
		var kinds []EventKind
		if err := e.Start(context.Background(), "spike", false, func(ev Event) { kinds = append(kinds, ev.Kind) }); err == nil {
			t.Fatal("want up failure")
		}
		if !containsKind(kinds, EventError) {
			t.Errorf("want EventError, got %v", kinds)
		}
	})

	// noClaude -> launchCommand returns nil (default shell). Also has-session error -> ensureTmuxSession error.
	t.Run("ensureTmuxSession error via has-session", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			switch {
			case strings.Contains(line, "devcontainer up"):
				return sysexec.Result{Stdout: upSuccessJSON}, nil, true
			case strings.Contains(line, "has-session"):
				return sysexec.Result{ExitCode: 2}, cmdErr(sysexec.Call{Name: "tmux"}, 2), true
			}
			return sysexec.Result{}, nil, false
		})
		e := testEngine(h)
		if err := e.Start(context.Background(), "spike", true, nil); err == nil {
			t.Fatal("want ensureTmuxSession error")
		}
	})
}

// -------------------- Exec --------------------

func TestExec(t *testing.T) {
	t.Run("in container", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if _, err := e.Exec(context.Background(), "feat-auth", "ls", "-la"); err != nil {
			t.Fatal(err)
		}
		last := e.Runner.(*sysexec.FakeRunner).LastCall().Line()
		if !strings.Contains(last, "devcontainer exec") || !strings.Contains(last, "ls -la") {
			t.Errorf("exec argv = %q", last)
		}
	})

	t.Run("host fallback", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if _, err := e.Exec(context.Background(), "spike", "echo", "hi there"); err != nil {
			t.Fatal(err)
		}
		last := e.Runner.(*sysexec.FakeRunner).LastCall()
		if last.Name != "sh" || len(last.Args) != 2 || last.Args[0] != "-c" {
			t.Fatalf("host argv = %+v", last)
		}
		script := last.Args[1]
		if !strings.Contains(script, "cd '/home/u/.weft/worktrees/app/spike'") {
			t.Errorf("script missing cd: %q", script)
		}
		if !strings.Contains(script, "'echo' 'hi there'") {
			t.Errorf("script missing joined cmd: %q", script)
		}
	})

	t.Run("no worktree errors", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if _, err := e.Exec(context.Background(), "ghost", "ls"); err == nil || !strings.Contains(err.Error(), "no worktree") {
			t.Fatalf("want no-worktree error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if _, err := e.Exec(context.Background(), "nope", "ls"); !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("reconcile error", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "worktree list") {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return reconcileHandler(c)
		}
		e := testEngine(h)
		if _, err := e.Exec(context.Background(), "feat-auth", "ls"); err == nil {
			t.Fatal("want reconcile error")
		}
	})
}

// -------------------- Repair --------------------

const tmuxWindowsWithGhost = "@1\tfeat-auth\t1\t1\tclaude\t0\t1720000000\n" +
	"@0\t0\t0\t0\tbash\t0\t1720000000\n" +
	"@2\tghost\t2\t0\tbash\t0\t1720000000\n"

func repairHandler(line string) (sysexec.Result, error, bool) {
	switch {
	case strings.Contains(line, "weft.session=app/ghost"):
		return sysexec.Result{Stdout: `{"ID":"c2","Image":"go","State":"exited","Labels":"weft.project=app,weft.session=app/ghost"}`}, nil, true
	case strings.Contains(line, "list-windows"):
		return sysexec.Result{Stdout: tmuxWindowsWithGhost}, nil, true
	}
	return sysexec.Result{}, nil, false
}

func TestRepair(t *testing.T) {
	t.Run("prunes and cleans orphan with container+window", func(t *testing.T) {
		e := testEngine(override(reconcileHandler, repairHandler))
		var steps []string
		rep, err := e.Repair(context.Background(), func(ev Event) {
			if ev.Kind == EventStep {
				steps = append(steps, ev.Step)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if !rep.PrunedWorktrees {
			t.Error("PrunedWorktrees = false, want true")
		}
		if rep.OrphanContainers != 1 {
			t.Errorf("OrphanContainers = %d, want 1", rep.OrphanContainers)
		}
		if rep.OrphanWindows != 1 {
			t.Errorf("OrphanWindows = %d, want 1", rep.OrphanWindows)
		}
		// non-orphans (feat-auth, spike) are never targeted.
		for _, c := range e.Runner.(*sysexec.FakeRunner).Calls {
			if strings.Contains(c.Line(), "kill-window") && strings.Contains(c.Line(), "feat-auth") {
				t.Error("non-orphan feat-auth window should not be killed")
			}
		}
		if !calledLike(e, "kill-window -t weft/app:ghost") {
			t.Error("expected ghost window kill")
		}
	})

	t.Run("prune error leaves PrunedWorktrees false", func(t *testing.T) {
		h := override(reconcileHandler, func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "worktree prune") {
				return sysexec.Result{ExitCode: 1}, errors.New("locked"), true
			}
			return repairHandler(line)
		})
		e := testEngine(h)
		rep, err := e.Repair(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.PrunedWorktrees {
			t.Error("PrunedWorktrees = true, want false")
		}
	})

	t.Run("reconcile error", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "worktree list") {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return reconcileHandler(c)
		}
		e := testEngine(h)
		if _, err := e.Repair(context.Background(), nil); err == nil {
			t.Fatal("want reconcile error")
		}
	})
}

// -------------------- runHook & pure helpers --------------------

func TestRunHook(t *testing.T) {
	t.Run("in container uses devcontainer exec", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if err := e.runHook(context.Background(), "/wt/x", true, "echo hi"); err != nil {
			t.Fatal(err)
		}
		last := e.Runner.(*sysexec.FakeRunner).LastCall().Line()
		if !strings.Contains(last, "devcontainer exec") || !strings.Contains(last, "sh -lc echo hi") {
			t.Errorf("hook argv = %q", last)
		}
	})

	t.Run("on host uses sh -c cd", func(t *testing.T) {
		e := testEngine(reconcileHandler)
		if err := e.runHook(context.Background(), "/wt/x", false, "echo hi"); err != nil {
			t.Fatal(err)
		}
		last := e.Runner.(*sysexec.FakeRunner).LastCall()
		if last.Kind != "mutate" || last.Name != "sh" {
			t.Fatalf("want sh mutate, got %+v", last)
		}
		if last.Args[1] != "cd '/wt/x' && echo hi" {
			t.Errorf("host hook script = %q", last.Args[1])
		}
	})
}

func TestPureHelpers(t *testing.T) {
	if got := singleQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("singleQuote = %q", got)
	}
	if got := shellJoin([]string{"a", "b c"}); got != `'a' 'b c'` {
		t.Errorf("shellJoin = %q", got)
	}
	if got := shellJoin(nil); got != "" {
		t.Errorf("shellJoin(nil) = %q", got)
	}
	if got := worktreePathOf(domain.Session{}); got != "" {
		t.Errorf("worktreePathOf(empty) = %q", got)
	}
	if got := worktreePathOf(domain.Session{Worktree: &domain.Worktree{Path: "/x"}}); got != "/x" {
		t.Errorf("worktreePathOf = %q", got)
	}
}

// -------------------- Reconcile: container-derived fields --------------------

func TestReconcileContainerLabels(t *testing.T) {
	const dockerPs2 = `{"ID":"c1","Image":"go","State":"running","Labels":"weft.project=app,weft.session=app/solo,weft.branch=weft/solo,weft.base_ref=develop,weft.created_at=2024-01-02T03:04:05Z"}
{"ID":"c2","Image":"go","State":"exited","Labels":"weft.project=app,weft.session=badkey"}
{"ID":"c3","Image":"go","State":"running","Labels":"weft.project=app,weft.session=app/feat-auth,weft.created_at=bogus"}`

	h := func(c sysexec.Call) (sysexec.Result, error) {
		switch {
		case strings.Contains(c.Line(), "docker ps"):
			return sysexec.Result{Stdout: dockerPs2}, nil
		default:
			return reconcileHandler(c)
		}
	}
	e := testEngine(h)
	sessions, err := e.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]domain.Session{}
	for _, s := range sessions {
		by[s.Name] = s
	}
	solo, ok := by["solo"]
	if !ok {
		t.Fatal("solo (orphan container) missing")
	}
	if solo.Branch != "weft/solo" {
		t.Errorf("solo.Branch = %q, want weft/solo", solo.Branch)
	}
	if solo.BaseRef != "develop" {
		t.Errorf("solo.BaseRef = %q, want develop", solo.BaseRef)
	}
	if solo.CreatedAt.IsZero() {
		t.Error("solo.CreatedAt should be parsed")
	}
	if _, exists := by["badkey"]; exists {
		t.Error("badkey should have been skipped (bad session key)")
	}
	if fa := by["feat-auth"]; !fa.CreatedAt.IsZero() {
		t.Error("feat-auth CreatedAt should be zero (unparseable timestamp)")
	}
}

func TestReconcileDegradesWhenTmuxDown(t *testing.T) {
	h := func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), "list-windows") {
			return sysexec.Result{ExitCode: 2}, cmdErr(c, 2)
		}
		return reconcileHandler(c)
	}
	e := testEngine(h)
	sessions, err := e.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.Window != nil {
			t.Errorf("no windows expected when tmux is down: %s", s.Name)
		}
	}
}

// -------------------- New: remaining branches --------------------

func TestNewBranches(t *testing.T) {
	// No devcontainer + existing tmux session (ensureTmuxSession returns early).
	t.Run("no devcontainer, existing tmux session", func(t *testing.T) {
		base := sagaHandler(true, "@7")
		h := override(base, func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "has-session") {
				return sysexec.Result{}, nil, true // session already exists
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		s, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if s.Container != nil {
			t.Error("no container expected with NoDevcontainer")
		}
		if calledLike(e, "new-session") {
			t.Error("should not create a tmux session when one exists")
		}
		if !calledLike(e, "new-window") {
			t.Error("expected new-window")
		}
	})

	t.Run("existing session rejected", func(t *testing.T) {
		base := sagaHandler(true, "@7")
		h := override(base, func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "worktree list") {
				return sysexec.Result{Stdout: "worktree /repo\nHEAD a\nbranch refs/heads/main\n\n" +
					"worktree /wt/feat\nHEAD b\nbranch refs/heads/weft/feat\n"}, nil, true
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		var kinds []EventKind
		_, err := e.New(context.Background(), NewSpec{Name: "feat"}, func(ev Event) { kinds = append(kinds, ev.Kind) })
		if !errors.Is(err, wefterr.ErrSessionExists) {
			t.Fatalf("want ErrSessionExists, got %v", err)
		}
		if !containsKind(kinds, EventError) {
			t.Errorf("want EventError, got %v", kinds)
		}
	})

	t.Run("FindSession error", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "worktree list") {
				return sysexec.Result{ExitCode: 1}, errors.New("git down"), true
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		if _, err := e.New(context.Background(), NewSpec{Name: "feat"}, nil); err == nil {
			t.Fatal("want FindSession error")
		}
	})

	t.Run("devcontainer config missing", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: sagaHandler(true, "@7")}
		e := sagaEngine(t, f)
		// Remove the devcontainer.json that sagaEngine created.
		if err := os.Remove(filepath.Join(e.Project.Root, ".devcontainer", "devcontainer.json")); err != nil {
			t.Fatal(err)
		}
		var kinds []EventKind
		_, err := e.New(context.Background(), NewSpec{Name: "feat"}, func(ev Event) { kinds = append(kinds, ev.Kind) })
		if !errors.Is(err, wefterr.ErrDevcontainerMissing) {
			t.Fatalf("want ErrDevcontainerMissing, got %v", err)
		}
		if !containsKind(kinds, EventError) {
			t.Errorf("want EventError, got %v", kinds)
		}
	})

	t.Run("branch exists without force rejected", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "rev-parse --verify") {
				return sysexec.Result{}, nil, true // branch exists
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		_, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, nil)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("want branch-exists error, got %v", err)
		}
	})

	t.Run("branch exists with force reuses it", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "rev-parse --verify") {
				return sysexec.Result{}, nil, true // branch exists
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		if _, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true, Force: true}, nil); err != nil {
			t.Fatal(err)
		}
		// worktree add for an existing branch has no "-b".
		for _, c := range f.Calls {
			if strings.Contains(c.Line(), "worktree add") && strings.Contains(c.Line(), "-b ") {
				t.Errorf("existing branch should not be created: %q", c.Line())
			}
		}
	})

	t.Run("BranchExists error", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "rev-parse --verify") {
				return sysexec.Result{ExitCode: 2}, cmdErr(sysexec.Call{Name: "git"}, 2), true
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		if _, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, nil); err == nil {
			t.Fatal("want BranchExists error")
		}
	})

	t.Run("MkdirAll parent error", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: sagaHandler(true, "@7")}
		e := sagaEngine(t, f)
		// Point the worktree root under a regular file so MkdirAll fails.
		file := filepath.Join(t.TempDir(), "afile")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.Cfg.Worktree.Root = filepath.Join(file, "{project}")
		_, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, nil)
		if err == nil || !strings.Contains(err.Error(), "create worktree parent") {
			t.Fatalf("want MkdirAll error, got %v", err)
		}
	})

	t.Run("AddWorktree error, no rollback", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "worktree add") {
				return sysexec.Result{ExitCode: 1}, errors.New("add failed"), true
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		_, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, nil)
		if err == nil || !strings.Contains(err.Error(), "git worktree add") {
			t.Fatalf("want AddWorktree error, got %v", err)
		}
	})

	t.Run("post_create hook success", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: sagaHandler(true, "@7")}
		e := sagaEngine(t, f)
		e.Cfg.Hooks.PostCreate = []string{"echo hi"}
		var steps []string
		_, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, func(ev Event) {
			if ev.Kind == EventStep {
				steps = append(steps, ev.Step)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if !containsStr(steps, "hook: echo hi") {
			t.Errorf("steps missing hook step: %v", steps)
		}
	})

	t.Run("post_create hook failure rolls back", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "hookboom") {
				return sysexec.Result{ExitCode: 1}, errors.New("hook died"), true
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		e.Cfg.Hooks.PostCreate = []string{"hookboom"}
		_, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, nil)
		if err == nil || !strings.Contains(err.Error(), "post_create hook") {
			t.Fatalf("want hook error, got %v", err)
		}
		if !calledLike(e, "worktree remove") {
			t.Error("rollback should have removed the worktree")
		}
	})

	t.Run("ensureTmuxSession error rolls back", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "has-session") {
				return sysexec.Result{ExitCode: 2}, cmdErr(sysexec.Call{Name: "tmux"}, 2), true
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		_, err := e.New(context.Background(), NewSpec{Name: "feat", NoDevcontainer: true}, nil)
		if err == nil || !strings.Contains(err.Error(), "tmux session") {
			t.Fatalf("want tmux session error, got %v", err)
		}
		if !calledLike(e, "worktree remove") {
			t.Error("rollback should have removed the worktree")
		}
	})

	t.Run("newwindow error rolls back container and worktree", func(t *testing.T) {
		h := override(sagaHandler(true, "@7"), func(line string) (sysexec.Result, error, bool) {
			if strings.Contains(line, "tmux new-window") {
				return sysexec.Result{ExitCode: 1}, errors.New("no window"), true
			}
			return sysexec.Result{}, nil, false
		})
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f) // devcontainer enabled -> container created then rolled back
		_, err := e.New(context.Background(), NewSpec{Name: "feat"}, nil)
		if err == nil || !strings.Contains(err.Error(), "tmux new-window") {
			t.Fatalf("want new-window error, got %v", err)
		}
		if !calledLike(e, "worktree remove") {
			t.Error("rollback should remove the worktree")
		}
		if !calledLike(e, "docker rm") && !calledLike(e, "docker ps") {
			t.Error("rollback should attempt container removal")
		}
	})

	t.Run("KeepOnFailure skips rollback", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: sagaHandler(false, "@7")}
		e := sagaEngine(t, f)
		_, err := e.New(context.Background(), NewSpec{Name: "feat", KeepOnFailure: true}, nil)
		if err == nil {
			t.Fatal("want up failure")
		}
		if calledLike(e, "worktree remove") {
			t.Error("KeepOnFailure should skip rollback (no worktree remove)")
		}
	})
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// -------------------- Remove: remaining branches --------------------

func TestRemoveBranches(t *testing.T) {
	t.Run("unpushed guard", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			switch {
			case strings.Contains(line, "worktree list"):
				return sysexec.Result{Stdout: "worktree /repo\nHEAD a\nbranch refs/heads/main\n\n" +
					"worktree /wt/feat\nHEAD b\nbranch refs/heads/weft/feat\n"}, nil
			case strings.Contains(line, "docker ps"), strings.Contains(line, "list-windows"):
				return sysexec.Result{}, nil
			case strings.Contains(line, "status --porcelain"):
				return sysexec.Result{}, nil // clean
			case strings.Contains(line, "rev-list"):
				return sysexec.Result{Stdout: "0\t3\n"}, nil // ahead 3
			default:
				return sysexec.Result{}, nil
			}
		}
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		err := e.Remove(context.Background(), RemoveSpec{Name: "feat"}, nil)
		if !errors.Is(err, wefterr.ErrDirtyWorktree) {
			t.Fatalf("want ErrDirtyWorktree (unpushed), got %v", err)
		}
	})

	t.Run("FindSession error", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "worktree list") {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return sysexec.Result{}, nil
		}
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		if err := e.Remove(context.Background(), RemoveSpec{Name: "feat"}, nil); err == nil {
			t.Fatal("want FindSession error")
		}
	})

	// Full teardown: window + container + branch + hooks, all succeeding.
	t.Run("full teardown", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			switch {
			case strings.Contains(line, "worktree list"):
				return sysexec.Result{Stdout: "worktree /repo\nHEAD a\nbranch refs/heads/main\n\n" +
					"worktree /wt/feat\nHEAD b\nbranch refs/heads/weft/feat\n"}, nil
			case strings.Contains(line, "docker ps"):
				return sysexec.Result{Stdout: `{"ID":"c9","Image":"go","State":"running","Labels":"weft.project=app,weft.session=app/feat"}`}, nil
			case strings.Contains(line, "list-windows"):
				return sysexec.Result{Stdout: "@3\tfeat\t3\t1\tclaude\t0\t1720000000\n"}, nil
			case strings.Contains(line, "status --porcelain"):
				return sysexec.Result{}, nil // clean
			case strings.Contains(line, "rev-list"):
				return sysexec.Result{Stdout: "0\t0\n"}, nil
			default:
				return sysexec.Result{}, nil
			}
		}
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		e.Cfg.Cleanup.DeleteBranch = true
		e.Cfg.Hooks.PreRemove = []string{"pre"}
		e.Cfg.Hooks.PostRemove = []string{"post"}
		var kinds []EventKind
		if err := e.Remove(context.Background(), RemoveSpec{Name: "feat"}, func(ev Event) { kinds = append(kinds, ev.Kind) }); err != nil {
			t.Fatal(err)
		}
		if !containsKind(kinds, EventDone) {
			t.Errorf("want EventDone, got %v", kinds)
		}
		for _, want := range []string{"kill-window -t weft/app:feat", "branch -d weft/feat", "devcontainer exec"} {
			if !calledLike(e, want) {
				t.Errorf("expected a call like %q", want)
			}
		}
	})

	// Every teardown step fails -> errors aggregated; hooks fail -> logged and continue.
	t.Run("aggregated failures", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			switch {
			case strings.Contains(line, "worktree list"):
				return sysexec.Result{Stdout: "worktree /repo\nHEAD a\nbranch refs/heads/main\n\n" +
					"worktree /wt/feat\nHEAD b\nbranch refs/heads/weft/feat\n"}, nil
			case strings.Contains(line, "docker ps"):
				return sysexec.Result{Stdout: `{"ID":"c9","Image":"go","State":"running","Labels":"weft.project=app,weft.session=app/feat"}`}, nil
			case strings.Contains(line, "list-windows"):
				return sysexec.Result{Stdout: "@3\tfeat\t3\t1\tclaude\t0\t1720000000\n"}, nil
			case strings.Contains(line, "kill-window"),
				strings.Contains(line, "docker rm"),
				strings.Contains(line, "worktree remove"),
				strings.Contains(line, "branch -"),
				strings.Contains(line, "devcontainer exec"): // pre_remove hook (in container)
				return sysexec.Result{ExitCode: 1}, errors.New("fail: " + line)
			case strings.HasPrefix(line, "sh -c"): // post_remove hook (host)
				return sysexec.Result{ExitCode: 1}, errors.New("hook fail")
			default:
				return sysexec.Result{}, nil
			}
		}
		f := &sysexec.FakeRunner{Handler: h}
		e := sagaEngine(t, f)
		e.Cfg.Cleanup.DeleteBranch = true
		e.Cfg.Hooks.PreRemove = []string{"pre"}
		e.Cfg.Hooks.PostRemove = []string{"post"}
		var kinds []EventKind
		err := e.Remove(context.Background(), RemoveSpec{Name: "feat", Force: true}, func(ev Event) { kinds = append(kinds, ev.Kind) })
		if err == nil {
			t.Fatal("want aggregated error")
		}
		if !containsKind(kinds, EventError) {
			t.Errorf("want EventError, got %v", kinds)
		}
	})
}

// calledLike reports whether the engine's fake runner recorded a call whose line
// contains sub.
func calledLike(e *Engine, sub string) bool {
	for _, c := range e.Runner.(*sysexec.FakeRunner).Calls {
		if strings.Contains(c.Line(), sub) {
			return true
		}
	}
	return false
}
