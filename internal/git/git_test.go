package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func newFake(handler func(sysexec.Call) (sysexec.Result, error)) (*sysexec.FakeRunner, *Exec) {
	f := &sysexec.FakeRunner{Handler: handler}
	return f, New(f, "/repo")
}

// errBoom is a generic non-CmdError failure used to exercise error propagation.
var errBoom = errors.New("boom")

func TestWorktreesParse(t *testing.T) {
	const out = `worktree /repo/bare
HEAD eeee5555
bare

worktree /repo
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
	if len(wts) != 5 {
		t.Fatalf("want 5 worktrees, got %d: %+v", len(wts), wts)
	}
	if !wts[0].Bare || wts[0].Path != "/repo/bare" {
		t.Errorf("worktree[0] should be bare: %+v", wts[0])
	}
	if wts[2].Branch != "weft/feat-auth" || wts[2].Path != "/home/u/.weft/worktrees/app/feat-auth" {
		t.Errorf("bad worktree[2]: %+v", wts[2])
	}
	if !wts[3].Detached || wts[3].Branch != "" {
		t.Errorf("worktree[3] should be detached: %+v", wts[3])
	}
	if !wts[4].Locked || wts[4].LockReason != "needs review" || !wts[4].Prunable {
		t.Errorf("worktree[4] lock/prune not parsed: %+v", wts[4])
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

func TestAheadBehindErrors(t *testing.T) {
	// rev-list fails: error propagates, counts are zero.
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if a, b, err := g.AheadBehind(context.Background(), "/wt", "origin/main"); err == nil || a != 0 || b != 0 {
		t.Fatalf("want error+zero, got a=%d b=%d err=%v", a, b, err)
	}

	// unexpected output shape (not exactly two fields) yields an error.
	_, g = newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "oops\n"}, nil
	})
	if a, b, err := g.AheadBehind(context.Background(), "/wt", "origin/main"); err == nil || a != 0 || b != 0 {
		t.Fatalf("want unexpected-output error, got a=%d b=%d err=%v", a, b, err)
	}
}

func TestToplevel(t *testing.T) {
	// success: stdout is trimmed and returned; the invocation is a plain Run.
	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "/home/u/repo\n"}, nil
	}}
	top, err := Toplevel(context.Background(), f, "/home/u/repo/sub")
	if err != nil {
		t.Fatal(err)
	}
	if top != "/home/u/repo" {
		t.Errorf("want /home/u/repo, got %q", top)
	}
	if got := f.LastCall(); got.Kind != "run" || !strings.Contains(got.Line(), "rev-parse --show-toplevel") {
		t.Errorf("unexpected call: %+v", got)
	}

	// failure: any error maps to ErrNotInRepo, empty path.
	f = &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	}}
	top, err = Toplevel(context.Background(), f, "/tmp")
	if !errors.Is(err, wefterr.ErrNotInRepo) || top != "" {
		t.Fatalf("want ErrNotInRepo+empty, got top=%q err=%v", top, err)
	}
}

func TestWorktreesError(t *testing.T) {
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if wts, err := g.Worktrees(context.Background()); err == nil || wts != nil {
		t.Fatalf("want error+nil, got wts=%v err=%v", wts, err)
	}
}

func TestAddWorktreeError(t *testing.T) {
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if err := g.AddWorktree(context.Background(), "/wt", "weft/x", "main", true); err == nil {
		t.Fatal("want error to propagate")
	}
}

func TestRemoveWorktree(t *testing.T) {
	// non-force: no --force flag, argv attaches path directly.
	f, g := newFake(nil)
	if err := g.RemoveWorktree(context.Background(), "/wt", false); err != nil {
		t.Fatal(err)
	}
	line := f.LastCall().Line()
	if f.LastCall().Kind != "mutate" {
		t.Errorf("RemoveWorktree should Mutate, got %q", f.LastCall().Kind)
	}
	if strings.Contains(line, "--force") {
		t.Errorf("non-force argv should not contain --force: %q", line)
	}
	if !strings.Contains(line, "worktree remove") || !strings.HasSuffix(line, "/wt") {
		t.Errorf("unexpected argv: %q", line)
	}

	// error propagates.
	_, g = newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if err := g.RemoveWorktree(context.Background(), "/wt", true); err == nil {
		t.Fatal("want error to propagate")
	}
}

func TestPrune(t *testing.T) {
	// success: mutates "worktree prune".
	f, g := newFake(nil)
	if err := g.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.LastCall(); got.Kind != "mutate" || !strings.Contains(got.Line(), "worktree prune") {
		t.Errorf("unexpected prune call: %+v", got)
	}

	// error propagates.
	_, g = newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if err := g.Prune(context.Background()); err == nil {
		t.Fatal("want error to propagate")
	}
}

func TestDeleteBranch(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		wantFlag string
	}{
		{"soft", false, "branch -d weft/x"},
		{"force", true, "branch -D weft/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, g := newFake(nil)
			if err := g.DeleteBranch(context.Background(), "weft/x", tt.force); err != nil {
				t.Fatal(err)
			}
			if got := f.LastCall(); got.Kind != "mutate" || !strings.Contains(got.Line(), tt.wantFlag) {
				t.Errorf("want %q via mutate, got %+v", tt.wantFlag, got)
			}
		})
	}

	// error propagates.
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if err := g.DeleteBranch(context.Background(), "weft/x", false); err == nil {
		t.Fatal("want error to propagate")
	}
}

func TestCurrentBranch(t *testing.T) {
	// success: trimmed.
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "feat-auth\n"}, nil
	})
	if b, err := g.CurrentBranch(context.Background()); err != nil || b != "feat-auth" {
		t.Fatalf("want feat-auth, got %q err=%v", b, err)
	}

	// error propagates, empty result.
	_, g = newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if b, err := g.CurrentBranch(context.Background()); err == nil || b != "" {
		t.Fatalf("want error+empty, got %q err=%v", b, err)
	}
}

func TestDefaultBranch(t *testing.T) {
	// gitSub reports the git subcommand invoked (args are "-C", root, <sub>, ...).
	gitSub := func(c sysexec.Call) string {
		if len(c.Args) >= 3 {
			return c.Args[2]
		}
		return ""
	}

	tests := []struct {
		name string
		// handler keyed by subcommand result
		symbolicOut string
		symbolicErr error
		configOut   string
		configErr   error
		want        string
	}{
		{"symbolic-ref", "origin/main\n", nil, "", nil, "main"},
		{"config-fallback", "", errBoom, "trunk\n", nil, "trunk"},
		{"both-error", "", errBoom, "", errBoom, "main"},
		{"empty-both", "\n", nil, "  \n", nil, "main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, g := newFake(func(c sysexec.Call) (sysexec.Result, error) {
				switch gitSub(c) {
				case "symbolic-ref":
					return sysexec.Result{Stdout: tt.symbolicOut}, tt.symbolicErr
				case "config":
					return sysexec.Result{Stdout: tt.configOut}, tt.configErr
				default:
					t.Fatalf("unexpected subcommand: %+v", c)
					return sysexec.Result{}, nil
				}
			})
			b, err := g.DefaultBranch(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if b != tt.want {
				t.Errorf("want %q, got %q", tt.want, b)
			}
		})
	}
}

func TestIsDirtyError(t *testing.T) {
	_, g := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	if dirty, err := g.IsDirty(context.Background(), "/wt"); err == nil || dirty {
		t.Fatalf("want error+clean, got dirty=%v err=%v", dirty, err)
	}
}
