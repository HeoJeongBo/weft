// Package git wraps the git CLI, focused on the worktree operations weft needs.
package git

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// Worktree is one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path       string
	Head       string
	Branch     string // short name; "" when detached or bare
	Detached   bool
	Bare       bool
	Locked     bool
	LockReason string
	Prunable   bool
}

// Git is the subset of git operations weft depends on.
type Git interface {
	Worktrees(ctx context.Context) ([]Worktree, error)
	AddWorktree(ctx context.Context, path, branch, base string, createBranch bool) error
	RemoveWorktree(ctx context.Context, path string, force bool) error
	Prune(ctx context.Context) error
	BranchExists(ctx context.Context, branch string) (bool, error)
	DeleteBranch(ctx context.Context, branch string, force bool) error
	CurrentBranch(ctx context.Context) (string, error)
	DefaultBranch(ctx context.Context) (string, error)
	IsDirty(ctx context.Context, worktreePath string) (bool, error)
	AheadBehind(ctx context.Context, worktreePath, upstream string) (ahead, behind int, err error)
}

// Exec is the real Git backed by a sysexec.Runner, rooted at a repository.
type Exec struct {
	r    sysexec.Runner
	root string
}

// New returns a Git rooted at the given repository top-level directory.
func New(r sysexec.Runner, root string) *Exec { return &Exec{r: r, root: root} }

// Toplevel returns the repository top-level containing dir, or ErrNotInRepo.
func Toplevel(ctx context.Context, r sysexec.Runner, dir string) (string, error) {
	res, err := r.Run(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", wefterr.ErrNotInRepo
	}
	return strings.TrimSpace(res.Stdout), nil
}

func (e *Exec) git(ctx context.Context, args ...string) (sysexec.Result, error) {
	return e.r.Run(ctx, "git", append([]string{"-C", e.root}, args...)...)
}

func (e *Exec) gitMutate(ctx context.Context, args ...string) (sysexec.Result, error) {
	return e.r.Mutate(ctx, "git", append([]string{"-C", e.root}, args...)...)
}

// Worktrees lists all worktrees of the repository.
func (e *Exec) Worktrees(ctx context.Context) ([]Worktree, error) {
	res, err := e.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(res.Stdout), nil
}

// AddWorktree creates a worktree at path. When createBranch is true it creates
// branch from base; otherwise it checks out the existing branch.
func (e *Exec) AddWorktree(ctx context.Context, path, branch, base string, createBranch bool) error {
	args := []string{"worktree", "add", "--quiet", path}
	if createBranch {
		args = append(args, "-b", branch, base)
	} else {
		args = append(args, branch)
	}
	_, err := e.gitMutate(ctx, args...)
	return err
}

// RemoveWorktree removes the worktree at path.
func (e *Exec) RemoveWorktree(ctx context.Context, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := e.gitMutate(ctx, args...)
	return err
}

// Prune removes worktree administrative files for deleted worktrees.
func (e *Exec) Prune(ctx context.Context) error {
	_, err := e.gitMutate(ctx, "worktree", "prune")
	return err
}

// BranchExists reports whether a local branch exists.
func (e *Exec) BranchExists(ctx context.Context, branch string) (bool, error) {
	_, err := e.git(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	if isExitCode(err, 1) {
		return false, nil
	}
	return false, err
}

// DeleteBranch deletes a local branch (-d, or -D when force).
func (e *Exec) DeleteBranch(ctx context.Context, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := e.gitMutate(ctx, "branch", flag, branch)
	return err
}

// CurrentBranch returns the checked-out branch of the repository root.
func (e *Exec) CurrentBranch(ctx context.Context) (string, error) {
	res, err := e.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// DefaultBranch returns the repository's default branch, best-effort ("main" fallback).
func (e *Exec) DefaultBranch(ctx context.Context) (string, error) {
	if res, err := e.git(ctx, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimSpace(res.Stdout); b != "" {
			return strings.TrimPrefix(b, "origin/"), nil
		}
	}
	if res, err := e.git(ctx, "config", "--get", "init.defaultBranch"); err == nil {
		if b := strings.TrimSpace(res.Stdout); b != "" {
			return b, nil
		}
	}
	return "main", nil
}

// IsDirty reports whether the worktree has uncommitted or untracked changes.
func (e *Exec) IsDirty(ctx context.Context, worktreePath string) (bool, error) {
	res, err := e.r.Run(ctx, "git", "-C", worktreePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(res.Stdout) != "", nil
}

// AheadBehind returns how many commits worktree HEAD is ahead/behind of upstream.
func (e *Exec) AheadBehind(ctx context.Context, worktreePath, upstream string) (ahead, behind int, err error) {
	res, err := e.r.Run(ctx, "git", "-C", worktreePath, "rev-list", "--left-right", "--count", upstream+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) != 2 {
		return 0, 0, errors.New("unexpected rev-list output: " + strings.TrimSpace(res.Stdout))
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return ahead, behind, nil
}

func parseWorktrees(out string) []Worktree {
	var wts []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil && cur.Path != "" {
			wts = append(wts, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			flush()
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		if cur == nil {
			cur = &Worktree{}
		}
		switch key {
		case "worktree":
			cur.Path = val
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked = true
			cur.LockReason = val
		case "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return wts
}

func isExitCode(err error, code int) bool {
	var ce *wefterr.CmdError
	return errors.As(err, &ce) && ce.ExitCode == code
}
