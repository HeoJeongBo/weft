package git

import (
	"context"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func newFake(handler func(sysexec.Call) (sysexec.Result, error)) (*sysexec.FakeRunner, *Exec) {
	f := &sysexec.FakeRunner{Handler: handler}
	return f, New(f, "/repo")
}

func TestWorktreesParse(t *testing.T) {
	const out = `worktree /repo
HEAD aaaa1111
branch refs/heads/main

worktree /home/u/.weft/worktrees/app/feat-auth
HEAD bbbb2222
branch refs/heads/weft/feat-auth

worktree /repo/detached
HEAD cccc3333
detached

worktree /repo/locked
HEAD dddd4444
branch refs/heads/weft/x
locked needs review
prunable gitdir gone
`
	_, g := newFake(func(c sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: out}, nil
	})

	wts, err := g.Worktrees(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 4 {
		t.Fatalf("want 4 worktrees, got %d: %+v", len(wts), wts)
	}
	if wts[1].Branch != "weft/feat-auth" || wts[1].Path != "/home/u/.weft/worktrees/app/feat-auth" {
		t.Errorf("bad worktree[1]: %+v", wts[1])
	}
	if !wts[2].Detached || wts[2].Branch != "" {
		t.Errorf("worktree[2] should be detached: %+v", wts[2])
	}
	if !wts[3].Locked || wts[3].LockReason != "needs review" || !wts[3].Prunable {
		t.Errorf("worktree[3] lock/prune not parsed: %+v", wts[3])
	}
}

func TestAddWorktreeArgs(t *testing.T) {
	tests := []struct {
		name         string
		createBranch bool
		wantContains []string
		wantAbsent   string
	}{
		{"create", true, []string{"worktree", "add", "--quiet", "/wt", "-b", "weft/x", "main"}, ""},
		{"attach", false, []string{"worktree", "add", "--quiet", "/wt", "weft/x"}, "-b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, g := newFake(nil)
			if err := g.AddWorktree(context.Background(), "/wt", "weft/x", "main", tt.createBranch); err != nil {
				t.Fatal(err)
			}
			line := f.LastCall().Line()
			if f.LastCall().Kind != "mutate" {
				t.Errorf("AddWorktree should Mutate, got %q", f.LastCall().Kind)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(line, want) {
					t.Errorf("argv %q missing %q", line, want)
				}
			}
			if tt.wantAbsent != "" && strings.Contains(line, " "+tt.wantAbsent+" ") {
				t.Errorf("argv %q should not contain %q", line, tt.wantAbsent)
			}
		})
	}
}

func TestRemoveWorktreeForce(t *testing.T) {
	f, g := newFake(nil)
	if err := g.RemoveWorktree(context.Background(), "/wt", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.LastCall().Line(), "--force") {
		t.Errorf("force flag missing: %q", f.LastCall().Line())
	}
}

func TestBranchExists(t *testing.T) {
	// Exists: rev-parse succeeds.
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "abcd\n"}, nil
	})
	if ok, err := g.BranchExists(context.Background(), "weft/x"); err != nil || !ok {
		t.Fatalf("want exists, got ok=%v err=%v", ok, err)
	}

	// Missing: rev-parse exits 1.
	_, g = newFake(func(c sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{ExitCode: 1}, &wefterr.CmdError{Cmd: "git", Args: c.Args, ExitCode: 1}
	})
	if ok, err := g.BranchExists(context.Background(), "weft/x"); err != nil || ok {
		t.Fatalf("want missing, got ok=%v err=%v", ok, err)
	}

	// Real error: exit 128 propagates.
	_, g = newFake(func(c sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{ExitCode: 128}, &wefterr.CmdError{Cmd: "git", Args: c.Args, ExitCode: 128}
	})
	if _, err := g.BranchExists(context.Background(), "weft/x"); err == nil {
		t.Fatal("want error to propagate on exit 128")
	}
}

func TestIsDirty(t *testing.T) {
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: " M file.go\n"}, nil
	})
	if dirty, err := g.IsDirty(context.Background(), "/wt"); err != nil || !dirty {
		t.Fatalf("want dirty, got %v err=%v", dirty, err)
	}

	_, g = newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: ""}, nil
	})
	if dirty, err := g.IsDirty(context.Background(), "/wt"); err != nil || dirty {
		t.Fatalf("want clean, got %v err=%v", dirty, err)
	}
}

func TestAheadBehind(t *testing.T) {
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "2\t5\n"}, nil // behind=2 ahead=5
	})
	ahead, behind, err := g.AheadBehind(context.Background(), "/wt", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if ahead != 5 || behind != 2 {
		t.Errorf("want ahead=5 behind=2, got ahead=%d behind=%d", ahead, behind)
	}
}
