package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/engine"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// testEnv holds the temp roots and the canned command outputs a fake runner
// answers with. Fields are read by base() at call time, so a test may tweak one
// (e.g. gitStatus="dirty") before invoking a command.
type testEnv struct {
	repoRoot    string
	wtRoot      string
	porcelain   string
	dockerPs    string
	tmuxWindows string
	gitStatus   string // `git status --porcelain` output; "" = clean
	revList     string // `git rev-list` output
}

func newTestEnv(t *testing.T) *testEnv { return newEnv(t, false) }

// newEnv writes a weft.yaml at a temp repo root (devcontainer toggled by
// dcEnabled) pointing worktrees at a second temp dir, and isolates user config.
func newEnv(t *testing.T, dcEnabled bool) *testEnv {
	t.Helper()
	repo := t.TempDir()
	wt := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no stray user config
	cfg := fmt.Sprintf(`version: 1
project:
  name: app
branch:
  prefix: "weft/"
worktree:
  root: "%s/{project}"
devcontainer:
  enabled: %t
  config: ".devcontainer/devcontainer.json"
tmux:
  session: "weft/{project}"
  window: "{name}"
claude:
  command: "claude"
  exec_in_container: true
cleanup:
  require_clean: true
`, wt, dcEnabled)
	if err := os.WriteFile(filepath.Join(repo, "weft.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		repoRoot:    repo,
		wtRoot:      wt,
		porcelain:   porcelainDoc(repo, "feat"),
		dockerPs:    dockerPsDoc([2]string{"feat", "running"}, [2]string{"ghost", "exited"}),
		tmuxWindows: windowsDoc("feat"),
		gitStatus:   "",
		revList:     "0\t0\n",
	}
}

// base answers every command a CLI flow issues against the canned env state.
func (env *testEnv) base(c sysexec.Call) (sysexec.Result, error) {
	line := c.Line()
	switch {
	case strings.Contains(line, "--show-toplevel"):
		return sysexec.Result{Stdout: env.repoRoot + "\n"}, nil
	case strings.Contains(line, "symbolic-ref"):
		return sysexec.Result{Stdout: "main\n"}, nil
	case strings.Contains(line, "config --get"):
		return sysexec.Result{Stdout: "main\n"}, nil
	case strings.Contains(line, "rev-parse --verify"): // branch existence probe
		return sysexec.Result{ExitCode: 1}, cmdErr(c, 1) // does not exist
	case strings.Contains(line, "worktree list"):
		return sysexec.Result{Stdout: env.porcelain}, nil
	case strings.Contains(line, "docker ps"):
		return sysexec.Result{Stdout: env.dockerPs}, nil
	case strings.Contains(line, "list-windows"):
		return sysexec.Result{Stdout: env.tmuxWindows}, nil
	case strings.Contains(line, "status --porcelain"):
		return sysexec.Result{Stdout: env.gitStatus}, nil
	case strings.Contains(line, "rev-list"):
		return sysexec.Result{Stdout: env.revList}, nil
	case strings.Contains(line, "has-session"):
		return sysexec.Result{ExitCode: 1}, cmdErr(c, 1) // absent -> new-session
	default:
		return sysexec.Result{}, nil
	}
}

// rootedAt delegates to base but reports root as the repository top-level.
func (env *testEnv) rootedAt(root string) func(sysexec.Call) (sysexec.Result, error) {
	return func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), "--show-toplevel") {
			return sysexec.Result{Stdout: root + "\n"}, nil
		}
		return env.base(c)
	}
}

func cmdErr(c sysexec.Call, code int) error {
	return &wefterr.CmdError{Cmd: c.Name, Args: c.Args, ExitCode: code}
}

func porcelainDoc(root string, names ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "worktree %s\nHEAD aaaaaaaaaaaa\nbranch refs/heads/main\n\n", filepath.Join(root, "main-wt"))
	for _, n := range names {
		fmt.Fprintf(&b, "worktree %s\nHEAD bbbbbbbbbbbb\nbranch refs/heads/weft/%s\n\n", filepath.Join(root, "wt", n), n)
	}
	return b.String()
}

func dockerPsDoc(entries ...[2]string) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, `{"ID":"c-%s","Image":"go:1.24","State":"%s","Labels":"weft.project=app,weft.session=app/%s"}`+"\n", e[0], e[1], e[0])
	}
	return b.String()
}

func windowsDoc(names ...string) string {
	var b strings.Builder
	for i, n := range names {
		fmt.Fprintf(&b, "@%d\t%s\t%d\t1\tclaude\t0\t1720000000\n", i+1, n, i+1)
	}
	return b.String()
}

// runCLI wires a fake runner into the command tree and executes args.
func runCLI(t *testing.T, handler func(sysexec.Call) (sysexec.Result, error), in string, args ...string) (string, string, error) {
	t.Helper()
	fake := &sysexec.FakeRunner{Handler: handler}
	saved := newRunner
	newRunner = func(dryRun bool, log *slog.Logger) sysexec.Runner { return fake }
	t.Cleanup(func() { newRunner = saved })

	root := NewRootCmd()
	var o, e bytes.Buffer
	root.SetOut(&o)
	root.SetErr(&e)
	root.SetIn(strings.NewReader(in))
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return o.String(), e.String(), err
}

// failTop makes only "rev-parse --show-toplevel" fail (ErrNotInRepo path).
func (env *testEnv) failTop(c sysexec.Call) (sysexec.Result, error) {
	if strings.Contains(c.Line(), "--show-toplevel") {
		return sysexec.Result{ExitCode: 128}, cmdErr(c, 128)
	}
	return env.base(c)
}

// failOn wraps base and returns err for any call whose line contains match.
func (env *testEnv) failOn(match string, err error) func(sysexec.Call) (sysexec.Result, error) {
	return func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), match) {
			return sysexec.Result{ExitCode: 1}, err
		}
		return env.base(c)
	}
}

func swapTTY(f func(io.Writer) bool) func() { s := isTTY; isTTY = f; return func() { isTTY = s } }
func swapDash(f func(context.Context, *engine.Engine) error) func() {
	s := runDashboard
	runDashboard = f
	return func() { runDashboard = s }
}

func swapExec(f func(context.Context, string, ...string) *exec.Cmd) func() {
	s := execCommand
	execCommand = f
	return func() { execCommand = s }
}

func unsetEnv(t *testing.T, k string) {
	if v, ok := os.LookupEnv(k); ok {
		t.Cleanup(func() { os.Setenv(k, v) })
		os.Unsetenv(k)
	}
}

// ---------------------------------------------------------------------------
// openEngine error path (every command)
// ---------------------------------------------------------------------------

func TestOpenEngineErrorPerCommand(t *testing.T) {
	commands := [][]string{
		{"ls"},
		{"status", "feat"},
		{"cd", "feat"},
		{"new", "feat"},
		{"rm", "feat", "--yes"},
		{"start", "feat"},
		{"stop", "feat"},
		{"exec", "feat", "echo"},
		{"init"},
		{"attach", "feat"},
		{"repair", "--yes"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			env := newTestEnv(t)
			_, _, err := runCLI(t, env.failTop, "", args...)
			if !errors.Is(err, wefterr.ErrNotInRepo) {
				t.Fatalf("want ErrNotInRepo, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ls
// ---------------------------------------------------------------------------

func TestLs(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "ls")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "feat") || !strings.Contains(out, "STATUS") {
			t.Fatalf("table missing content:\n%s", out)
		}
	})
	t.Run("empty", func(t *testing.T) {
		env := newTestEnv(t)
		env.porcelain = porcelainDoc(env.repoRoot)
		env.dockerPs = ""
		env.tmuxWindows = ""
		out, _, err := runCLI(t, env.base, "", "ls")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "no sessions yet") {
			t.Fatalf("want empty hint, got %q", out)
		}
	})
	t.Run("json", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "ls", "--json")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `"Name"`) {
			t.Fatalf("want json, got %q", out)
		}
	})
	t.Run("reconcile_error", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.failOn("worktree list", errors.New("boom")), "", "ls")
		if err == nil {
			t.Fatal("want reconcile error")
		}
	})
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func TestStatus(t *testing.T) {
	t.Run("detail", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "status", "feat")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "feat") || !strings.Contains(out, "branch") {
			t.Fatalf("detail missing:\n%s", out)
		}
	})
	t.Run("not_found", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "status", "nope")
		if !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	})
	t.Run("json", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "status", "feat", "--json")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `"Name": "feat"`) {
			t.Fatalf("want json, got %q", out)
		}
	})
	t.Run("find_error", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.failOn("worktree list", errors.New("boom")), "", "status", "feat")
		if err == nil {
			t.Fatal("want find error")
		}
	})
}

// ---------------------------------------------------------------------------
// cd
// ---------------------------------------------------------------------------

func TestCd(t *testing.T) {
	t.Run("path", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "cd", "feat")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, filepath.Join(env.repoRoot, "wt", "feat")) {
			t.Fatalf("want worktree path, got %q", out)
		}
	})
	t.Run("not_found", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "cd", "nope")
		if !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	})
	t.Run("no_worktree", func(t *testing.T) {
		env := newTestEnv(t) // ghost is a container-only orphan
		_, _, err := runCLI(t, env.base, "", "cd", "ghost")
		if err == nil || !strings.Contains(err.Error(), "no worktree") {
			t.Fatalf("want no-worktree error, got %v", err)
		}
	})
	t.Run("find_error", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.failOn("worktree list", errors.New("boom")), "", "cd", "feat")
		if err == nil {
			t.Fatal("want find error")
		}
	})
}

// ---------------------------------------------------------------------------
// new
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	t.Run("invalid_name", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "new", "bad name")
		if err == nil || !strings.Contains(err.Error(), "invalid session name") {
			t.Fatalf("want invalid name, got %v", err)
		}
	})
	t.Run("exists", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "new", "feat")
		if !errors.Is(err, wefterr.ErrSessionExists) {
			t.Fatalf("want exists, got %v", err)
		}
	})
	t.Run("devcontainer_missing", func(t *testing.T) {
		env := newEnv(t, true) // dc enabled, no devcontainer.json present
		_, _, err := runCLI(t, env.base, "", "new", "fresh")
		if !errors.Is(err, wefterr.ErrDevcontainerMissing) {
			t.Fatalf("want dc missing, got %v", err)
		}
	})
	t.Run("success", func(t *testing.T) {
		env := newTestEnv(t)
		out, errOut, err := runCLI(t, env.base, "", "new", "fresh")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "ready") || !strings.Contains(out, "weft/fresh") {
			t.Fatalf("want ready line, got %q", out)
		}
		if !strings.Contains(errOut, "attach with: weft attach fresh") {
			t.Fatalf("want attach hint, got %q", errOut)
		}
	})
	t.Run("engine_new_error", func(t *testing.T) {
		env := newTestEnv(t)
		// Fail the worktree add mutation so New returns an error.
		_, _, err := runCLI(t, env.failOn("worktree add", errors.New("add failed")), "", "new", "fresh")
		if err == nil {
			t.Fatal("want new error")
		}
	})
	t.Run("attach", func(t *testing.T) {
		env := newTestEnv(t)
		t.Setenv("TMUX", "") // outside tmux -> exec path
		defer swapExec(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
		})()

		live := false
		h := func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			switch {
			case strings.Contains(line, "new-window"):
				live = true
				return sysexec.Result{Stdout: "@9\n"}, nil
			case strings.Contains(line, "worktree list"):
				if live {
					return sysexec.Result{Stdout: porcelainDoc(env.repoRoot, "feat", "fresh")}, nil
				}
				return sysexec.Result{Stdout: env.porcelain}, nil
			case strings.Contains(line, "list-windows"):
				if live {
					return sysexec.Result{Stdout: windowsDoc("feat", "fresh")}, nil
				}
				return sysexec.Result{Stdout: env.tmuxWindows}, nil
			default:
				return env.base(c)
			}
		}
		out, _, err := runCLI(t, h, "", "new", "fresh", "--attach")
		if err != nil {
			t.Fatalf("attach after new: %v", err)
		}
		if !strings.Contains(out, "ready") {
			t.Fatalf("want ready, got %q", out)
		}
	})
}

// ---------------------------------------------------------------------------
// rm
// ---------------------------------------------------------------------------

func TestRm(t *testing.T) {
	t.Run("confirm_no", func(t *testing.T) {
		env := newTestEnv(t)
		_, errOut, err := runCLI(t, env.base, "n\n", "rm", "feat")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "aborted") {
			t.Fatalf("want aborted, got %q", errOut)
		}
	})
	t.Run("confirm_yes", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "y\n", "rm", "feat")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "removed") {
			t.Fatalf("want removed, got %q", out)
		}
	})
	t.Run("yes_flag", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "rm", "feat", "--yes")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "removed") {
			t.Fatalf("want removed, got %q", out)
		}
	})
	t.Run("dirty_guard", func(t *testing.T) {
		env := newTestEnv(t)
		env.gitStatus = "M file.go\n"
		_, _, err := runCLI(t, env.base, "", "rm", "feat", "--yes")
		if !errors.Is(err, wefterr.ErrDirtyWorktree) {
			t.Fatalf("want dirty, got %v", err)
		}
	})
	t.Run("force_dirty", func(t *testing.T) {
		env := newTestEnv(t)
		env.gitStatus = "M file.go\n"
		out, _, err := runCLI(t, env.base, "", "rm", "feat", "--yes", "--force")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "removed") {
			t.Fatalf("want removed, got %q", out)
		}
	})
	t.Run("delete_branch", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "rm", "feat", "--yes", "--delete-branch")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "removed") {
			t.Fatalf("want removed, got %q", out)
		}
	})
	t.Run("engine_error", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.failOn("kill-window", errors.New("kw failed")), "", "rm", "feat", "--yes")
		if err == nil {
			t.Fatal("want remove error")
		}
	})
}

// ---------------------------------------------------------------------------
// start / stop
// ---------------------------------------------------------------------------

func TestStartStop(t *testing.T) {
	t.Run("start_success", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "start", "feat")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "started") {
			t.Fatalf("want started, got %q", out)
		}
	})
	t.Run("start_not_found", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "start", "nope")
		if !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	})
	t.Run("stop_success", func(t *testing.T) {
		env := newTestEnv(t)
		out, errOut, err := runCLI(t, env.base, "", "stop", "feat")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "stopped") {
			t.Fatalf("want stopped, got %q", out)
		}
		if !strings.Contains(errOut, "stop container") {
			t.Fatalf("want progress step, got %q", errOut)
		}
	})
	t.Run("stop_not_found", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "stop", "nope")
		if !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	})
	t.Run("stop_verbose_log", func(t *testing.T) {
		env := newTestEnv(t)
		env.dockerPs = "" // feat has no container -> sink.log path
		_, errOut, err := runCLI(t, env.base, "", "-v", "stop", "feat")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "no container to stop") {
			t.Fatalf("want verbose log, got %q", errOut)
		}
	})
}

// ---------------------------------------------------------------------------
// exec
// ---------------------------------------------------------------------------

func TestExec(t *testing.T) {
	t.Run("strip_dashes", func(t *testing.T) {
		env := newTestEnv(t)
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "devcontainer exec") {
				return sysexec.Result{Stdout: "OUT", Stderr: "ERR"}, nil
			}
			return env.base(c)
		}
		out, errOut, err := runCLI(t, h, "", "exec", "feat", "--", "echo", "hi")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "OUT") || !strings.Contains(errOut, "ERR") {
			t.Fatalf("want OUT/ERR, got out=%q err=%q", out, errOut)
		}
	})
	t.Run("no_dash", func(t *testing.T) {
		env := newTestEnv(t)
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "devcontainer exec") {
				return sysexec.Result{Stdout: "hey"}, nil
			}
			return env.base(c)
		}
		out, _, err := runCLI(t, h, "", "exec", "feat", "ls")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "hey") {
			t.Fatalf("want output, got %q", out)
		}
	})
	t.Run("no_command", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "exec", "feat", "--")
		if err == nil || !strings.Contains(err.Error(), "no command given") {
			t.Fatalf("want no command, got %v", err)
		}
	})
	t.Run("exec_error", func(t *testing.T) {
		env := newTestEnv(t)
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "devcontainer exec") {
				return sysexec.Result{ExitCode: 2}, cmdErr(c, 2)
			}
			return env.base(c)
		}
		_, _, err := runCLI(t, h, "", "exec", "feat", "false")
		if err == nil {
			t.Fatal("want exec error")
		}
	})
}

// ---------------------------------------------------------------------------
// init
// ---------------------------------------------------------------------------

func TestInit(t *testing.T) {
	t.Run("already_exists", func(t *testing.T) {
		env := newTestEnv(t) // weft.yaml already at repoRoot
		_, _, err := runCLI(t, env.base, "", "init")
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("want already-exists, got %v", err)
		}
	})
	t.Run("force_overwrite", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "init", "--force")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "wrote") || !strings.Contains(out, "no devcontainer.json detected") {
			t.Fatalf("want wrote+note, got %q", out)
		}
	})
	t.Run("success_no_dc", func(t *testing.T) {
		env := newTestEnv(t)
		fresh := t.TempDir()
		out, _, err := runCLI(t, env.rootedAt(fresh), "", "init")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "wrote") || !strings.Contains(out, "no devcontainer.json detected") {
			t.Fatalf("want wrote+note, got %q", out)
		}
		if _, err := os.Stat(filepath.Join(fresh, "weft.yaml")); err != nil {
			t.Fatalf("weft.yaml not written: %v", err)
		}
	})
	t.Run("write_error", func(t *testing.T) {
		env := newTestEnv(t)
		// Root points at a directory that does not exist, so WriteFile fails.
		ghost := filepath.Join(t.TempDir(), "does-not-exist")
		_, _, err := runCLI(t, env.rootedAt(ghost), "", "init")
		if err == nil {
			t.Fatal("want write error")
		}
	})
	t.Run("success_with_dc", func(t *testing.T) {
		env := newTestEnv(t)
		fresh := t.TempDir()
		if err := os.MkdirAll(filepath.Join(fresh, ".devcontainer"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fresh, ".devcontainer", "devcontainer.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := runCLI(t, env.rootedAt(fresh), "", "init")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "wrote") {
			t.Fatalf("want wrote, got %q", out)
		}
		if strings.Contains(out, "no devcontainer.json detected") {
			t.Fatalf("should not warn when dc present: %q", out)
		}
	})
}

func TestDetectDevcontainer(t *testing.T) {
	t.Run("dot_file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".devcontainer.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		p, ok := detectDevcontainer(root)
		if !ok || p != ".devcontainer.json" {
			t.Fatalf("got %q, %v", p, ok)
		}
	})
	t.Run("none", func(t *testing.T) {
		p, ok := detectDevcontainer(t.TempDir())
		if ok || p != ".devcontainer/devcontainer.json" {
			t.Fatalf("got %q, %v", p, ok)
		}
	})
}

func TestRenderInitConfig(t *testing.T) {
	on := renderInitConfig("app", "main", ".devcontainer/devcontainer.json", true)
	if !strings.Contains(on, "enabled: true") {
		t.Errorf("want enabled true:\n%s", on)
	}
	off := renderInitConfig("app", "main", ".devcontainer/devcontainer.json", false)
	if !strings.Contains(off, "enabled: false") {
		t.Errorf("want enabled false:\n%s", off)
	}
}

// ---------------------------------------------------------------------------
// attach
// ---------------------------------------------------------------------------

func TestAttach(t *testing.T) {
	t.Run("find_error", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.failOn("worktree list", errors.New("boom")), "", "attach", "feat")
		if err == nil {
			t.Fatal("want find error")
		}
	})
	t.Run("not_found", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.base, "", "attach", "nope")
		if !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	})
	t.Run("no_window", func(t *testing.T) {
		env := newTestEnv(t)
		env.tmuxWindows = "" // feat has no tmux window
		_, _, err := runCLI(t, env.base, "", "attach", "feat")
		if err == nil || !strings.Contains(err.Error(), "no tmux window") {
			t.Fatalf("want no-window error, got %v", err)
		}
	})
	t.Run("inside_tmux", func(t *testing.T) {
		env := newTestEnv(t)
		t.Setenv("TMUX", "/tmp/tmux-1,1,0")
		_, _, err := runCLI(t, env.base, "", "attach", "feat")
		if err != nil {
			t.Fatalf("switch-client should succeed, got %v", err)
		}
	})
	t.Run("outside_tmux_success", func(t *testing.T) {
		env := newTestEnv(t)
		t.Setenv("TMUX", "")
		defer swapExec(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
		})()
		_, _, err := runCLI(t, env.base, "", "attach", "feat")
		if err != nil {
			t.Fatalf("attach should succeed, got %v", err)
		}
	})
	t.Run("outside_tmux_failure", func(t *testing.T) {
		env := newTestEnv(t)
		t.Setenv("TMUX", "")
		defer swapExec(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		})()
		_, _, err := runCLI(t, env.base, "", "attach", "feat")
		if err == nil {
			t.Fatal("want attach failure")
		}
	})
	t.Run("start_flag", func(t *testing.T) {
		env := newTestEnv(t)
		t.Setenv("TMUX", "/tmp/tmux-1,1,0") // switch-client path after start
		_, _, err := runCLI(t, env.base, "", "attach", "feat", "--start")
		if err != nil {
			t.Fatalf("attach --start should succeed, got %v", err)
		}
	})
	t.Run("start_flag_error", func(t *testing.T) {
		env := newTestEnv(t)
		// Start fails on the not-found session.
		_, _, err := runCLI(t, env.base, "", "attach", "nope", "--start")
		if !errors.Is(err, wefterr.ErrSessionNotFound) {
			t.Fatalf("want not found from start, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// repair
// ---------------------------------------------------------------------------

func TestRepair(t *testing.T) {
	t.Run("confirm_no", func(t *testing.T) {
		env := newTestEnv(t)
		_, errOut, err := runCLI(t, env.base, "n\n", "repair")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "aborted") {
			t.Fatalf("want aborted, got %q", errOut)
		}
	})
	t.Run("confirm_yes", func(t *testing.T) {
		env := newTestEnv(t)
		env.tmuxWindows = windowsDoc("feat", "ghost") // ghost orphan window too
		out, _, err := runCLI(t, env.base, "y\n", "repair")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "repaired") || !strings.Contains(out, "pruned worktree metadata") {
			t.Fatalf("want repaired+pruned, got %q", out)
		}
	})
	t.Run("yes_flag", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "", "repair", "--yes")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "repaired") {
			t.Fatalf("want repaired, got %q", out)
		}
	})
	t.Run("repair_error", func(t *testing.T) {
		env := newTestEnv(t)
		_, _, err := runCLI(t, env.failOn("worktree list", errors.New("boom")), "", "repair", "--yes")
		if err == nil {
			t.Fatal("want repair error")
		}
	})
}

// ---------------------------------------------------------------------------
// version
// ---------------------------------------------------------------------------

func TestVersionCmd(t *testing.T) {
	env := newTestEnv(t)
	out, _, err := runCLI(t, env.base, "", "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "weft") {
		t.Fatalf("want version, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// root RunE branches
// ---------------------------------------------------------------------------

func TestRootRunE(t *testing.T) {
	t.Run("no_tty_runs_ls", func(t *testing.T) {
		env := newTestEnv(t)
		out, _, err := runCLI(t, env.base, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "feat") {
			t.Fatalf("want ls output, got %q", out)
		}
	})
	t.Run("tty_open_error", func(t *testing.T) {
		env := newTestEnv(t)
		defer swapTTY(func(io.Writer) bool { return true })()
		_, _, err := runCLI(t, env.failTop, "")
		if !errors.Is(err, wefterr.ErrNotInRepo) {
			t.Fatalf("want open error, got %v", err)
		}
	})
	t.Run("tty_dashboard_success", func(t *testing.T) {
		env := newTestEnv(t)
		defer swapTTY(func(io.Writer) bool { return true })()
		defer swapDash(func(context.Context, *engine.Engine) error { return nil })()
		_, _, err := runCLI(t, env.base, "")
		if err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})
	t.Run("tty_dashboard_error", func(t *testing.T) {
		env := newTestEnv(t)
		sentinel := errors.New("dashboard boom")
		defer swapTTY(func(io.Writer) bool { return true })()
		defer swapDash(func(context.Context, *engine.Engine) error { return sentinel })()
		_, _, err := runCLI(t, env.base, "")
		if !errors.Is(err, sentinel) {
			t.Fatalf("want sentinel, got %v", err)
		}
	})
}

// TestOpenEngineGetwdError covers openEngine's default newRunner closure (left
// un-overridden here) and its getwd error branch deterministically via the getwd
// seam.
func TestOpenEngineGetwdError(t *testing.T) {
	orig := getwd
	t.Cleanup(func() { getwd = orig })
	getwd = func() (string, error) { return "", errors.New("no-cwd-here") }

	root := NewRootCmd()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"ls"})

	if err := root.ExecuteContext(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "no-cwd-here") {
		t.Fatalf("want getwd error, got %v", err)
	}
}

func TestExitCode(t *testing.T) {
	if ExitCode(nil) != wefterr.CodeOK {
		t.Errorf("nil -> %d", ExitCode(nil))
	}
	if ExitCode(wefterr.ErrDependencyMissing) != wefterr.CodeDependency {
		t.Errorf("dep -> %d", ExitCode(wefterr.ErrDependencyMissing))
	}
}

// ---------------------------------------------------------------------------
// Execute (fang)
// ---------------------------------------------------------------------------

func TestExecute(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		var o, e bytes.Buffer
		if err := Execute(context.Background(), []string{"--help"}, &o, &e); err != nil {
			t.Fatalf("--help: %v", err)
		}
	})
	t.Run("version", func(t *testing.T) {
		var o, e bytes.Buffer
		if err := Execute(context.Background(), []string{"version"}, &o, &e); err != nil {
			t.Fatalf("version: %v", err)
		}
	})
	t.Run("bogus", func(t *testing.T) {
		var o, e bytes.Buffer
		if err := Execute(context.Background(), []string{"totallybogusflag---"}, &o, &e); err == nil {
			t.Fatal("want error for bogus command")
		}
	})
}

// ---------------------------------------------------------------------------
// doctor
// ---------------------------------------------------------------------------

func swapDoctor(t *testing.T,
	lp func(string) (string, error),
	rc func(context.Context, string, ...string) ([]byte, error),
	ro func(context.Context, string, ...string) error,
) {
	t.Helper()
	slp, src, sro := lookPath, runCommand, runOK
	lookPath, runCommand, runOK = lp, rc, ro
	t.Cleanup(func() { lookPath, runCommand, runOK = slp, src, sro })
}

func TestDoctor(t *testing.T) {
	found := func(name string) (string, error) { return "/usr/bin/" + name, nil }
	ver := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		return []byte(name + " 1.2.3\n"), nil
	}
	daemonUp := func(context.Context, string, ...string) error { return nil }

	t.Run("all_good", func(t *testing.T) {
		swapDoctor(t, found, ver, daemonUp)
		out, _, err := runCLI(t, nil, "", "doctor")
		if err != nil {
			t.Fatalf("want ok, got %v", err)
		}
		if !strings.Contains(out, "all required dependencies present") {
			t.Fatalf("want all present, got %q", out)
		}
	})
	t.Run("required_missing", func(t *testing.T) {
		lp := func(name string) (string, error) {
			if name == "git" {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		}
		swapDoctor(t, lp, ver, daemonUp)
		out, _, err := runCLI(t, nil, "", "doctor")
		if !errors.Is(err, wefterr.ErrDependencyMissing) {
			t.Fatalf("want dep missing, got %v", err)
		}
		if !strings.Contains(out, "missing") || !strings.Contains(out, "not found on PATH") {
			t.Fatalf("want missing detail+hint, got %q", out)
		}
	})
	t.Run("optional_missing", func(t *testing.T) {
		lp := func(name string) (string, error) {
			if name == "node" {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		}
		swapDoctor(t, lp, ver, daemonUp)
		out, _, err := runCLI(t, nil, "", "doctor")
		if err != nil {
			t.Fatalf("optional missing should not fail, got %v", err)
		}
		if !strings.Contains(out, "all required dependencies present") {
			t.Fatalf("want all present, got %q", out)
		}
	})
	t.Run("daemon_down", func(t *testing.T) {
		down := func(context.Context, string, ...string) error { return errors.New("no daemon") }
		swapDoctor(t, found, ver, down)
		_, _, err := runCLI(t, nil, "", "doctor")
		if !errors.Is(err, wefterr.ErrDependencyMissing) {
			t.Fatalf("want dep missing, got %v", err)
		}
	})
	t.Run("json", func(t *testing.T) {
		swapDoctor(t, found, ver, daemonUp)
		out, _, err := runCLI(t, nil, "", "doctor", "--json")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `"name": "git"`) {
			t.Fatalf("want json, got %q", out)
		}
	})
	t.Run("version_error_keeps_installed", func(t *testing.T) {
		verErr := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("boom") }
		swapDoctor(t, found, verErr, daemonUp)
		out, _, err := runCLI(t, nil, "", "doctor")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "installed") {
			t.Fatalf("want installed, got %q", out)
		}
	})
	t.Run("docker_not_installed", func(t *testing.T) {
		lp := func(name string) (string, error) {
			if name == "docker" {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		}
		swapDoctor(t, lp, ver, daemonUp)
		out, _, err := runCLI(t, nil, "", "doctor")
		if !errors.Is(err, wefterr.ErrDependencyMissing) {
			t.Fatalf("want dep missing, got %v", err)
		}
		if !strings.Contains(out, "docker not installed") {
			t.Fatalf("want docker-not-installed detail, got %q", out)
		}
	})
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestReportChecksJSONError(t *testing.T) {
	err := reportChecks(failWriter{}, []checkResult{{Name: "git", OK: true}}, true, false)
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("want encode error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// colorEnabled / isTerminal
// ---------------------------------------------------------------------------

func cmdWithNoColor(v bool) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("no-color", v, "")
	var buf bytes.Buffer
	c.SetOut(&buf)
	return c
}

func TestColorEnabled(t *testing.T) {
	t.Run("no_color_flag", func(t *testing.T) {
		if colorEnabled(cmdWithNoColor(true)) {
			t.Error("want false with --no-color")
		}
	})
	t.Run("no_color_env", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if colorEnabled(cmdWithNoColor(false)) {
			t.Error("want false with NO_COLOR")
		}
	})
	t.Run("not_terminal", func(t *testing.T) {
		unsetEnv(t, "NO_COLOR")
		if colorEnabled(cmdWithNoColor(false)) {
			t.Error("want false for buffer out")
		}
	})
}

func TestIsTerminal(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("buffer is not a terminal")
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !isTerminal(f) {
		t.Error("/dev/null should be a char device")
	}
	cf, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	cf.Close()
	if isTerminal(cf) {
		t.Error("closed file should Stat-error to false")
	}
}

// ---------------------------------------------------------------------------
// render.go pure helpers
// ---------------------------------------------------------------------------

func TestColorize(t *testing.T) {
	if got := colorize("x", ansiRed, false); got != "x" {
		t.Errorf("off = %q", got)
	}
	if got := colorize("x", ansiRed, true); got != ansiRed+"x"+ansiReset {
		t.Errorf("on = %q", got)
	}
}

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := printJSON(&buf, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"a": 1`) {
		t.Fatalf("got %q", buf.String())
	}
}

func TestStatusGlyphAndBadge(t *testing.T) {
	cases := []domain.SessionStatus{
		domain.StatusReady, domain.StatusStarting, domain.StatusStopped,
		domain.StatusPartial, domain.StatusOrphaned, domain.StatusUnknown,
		domain.SessionStatus("weird"),
	}
	for _, s := range cases {
		g, code := statusGlyph(s)
		if g == "" || code == "" {
			t.Errorf("%s -> empty glyph/code", s)
		}
	}
	if statusBadge(domain.StatusReady, true) == "" {
		t.Error("badge empty")
	}
}

func TestContainerCell(t *testing.T) {
	if containerCell(domain.Session{}) != "—" {
		t.Error("nil -> dash")
	}
	if got := containerCell(domain.Session{Container: &domain.Container{State: "running"}}); got != "up" {
		t.Errorf("running -> %q", got)
	}
	if got := containerCell(domain.Session{Container: &domain.Container{State: "exited"}}); got != "down" {
		t.Errorf("exited -> %q", got)
	}
	if got := containerCell(domain.Session{Container: &domain.Container{State: "dead"}}); got != "down" {
		t.Errorf("dead -> %q", got)
	}
	if got := containerCell(domain.Session{Container: &domain.Container{State: "paused"}}); got != "paused" {
		t.Errorf("paused -> %q", got)
	}
}

func TestClaudeCell(t *testing.T) {
	if claudeCell(domain.ClaudeNone) != "—" {
		t.Error("none -> dash")
	}
	if claudeCell(domain.ClaudeRunning) != "running" {
		t.Error("running")
	}
}

func TestPrintSessionsTable(t *testing.T) {
	var empty bytes.Buffer
	printSessionsTable(&empty, nil, false)
	if !strings.Contains(empty.String(), "no sessions yet") {
		t.Fatalf("empty: %q", empty.String())
	}
	var buf bytes.Buffer
	printSessionsTable(&buf, []domain.Session{
		{Name: "a", Branch: "weft/a", Status: domain.StatusReady},
		{Name: "b", Branch: "weft/b", Status: domain.StatusStopped, Container: &domain.Container{State: "running"}, Claude: domain.ClaudeIdle},
	}, true)
	if !strings.Contains(buf.String(), "a") || !strings.Contains(buf.String(), "STATUS") {
		t.Fatalf("table: %q", buf.String())
	}
}

func TestPrintSessionDetail(t *testing.T) {
	proj := domain.Project{DefaultBranch: "main", TmuxSession: "weft/app"}
	// worktree nil, container nil, window nil.
	var b1 bytes.Buffer
	printSessionDetail(&b1, domain.Session{Name: "a", Status: domain.StatusPartial}, proj, false)
	if !strings.Contains(b1.String(), "branch") {
		t.Fatalf("b1: %q", b1.String())
	}
	// full: dirty worktree + container + window.
	var b2 bytes.Buffer
	printSessionDetail(&b2, domain.Session{
		Name:      "a",
		Status:    domain.StatusReady,
		Worktree:  &domain.Worktree{Path: "/wt", Dirty: true, Ahead: 1, Behind: 2},
		Container: &domain.Container{ID: "0123456789abcdef", State: "running", Image: "go"},
		Window:    &domain.Window{Index: 3},
	}, proj, true)
	s := b2.String()
	if !strings.Contains(s, "dirty") || !strings.Contains(s, "container") || !strings.Contains(s, "tmux") {
		t.Fatalf("b2: %q", s)
	}
	// clean worktree branch (non-dirty path).
	var b3 bytes.Buffer
	printSessionDetail(&b3, domain.Session{Name: "a", Worktree: &domain.Worktree{Path: "/wt"}}, proj, false)
	if strings.Contains(b3.String(), "dirty") {
		t.Fatalf("b3 should be clean: %q", b3.String())
	}
}

func TestTruncate(t *testing.T) {
	if truncate("ab", 5) != "ab" {
		t.Error("short")
	}
	if truncate("abc", 1) != "a" {
		t.Error("n<=1")
	}
	if truncate("abcdef", 3) != "ab…" {
		t.Errorf("middle -> %q", truncate("abcdef", 3))
	}
}

func TestShort(t *testing.T) {
	if short("0123456789abcdef") != "0123456789ab" {
		t.Errorf("long -> %q", short("0123456789abcdef"))
	}
	if short("abc") != "abc" {
		t.Error("short")
	}
}

// ---------------------------------------------------------------------------
// doctor.go pure helpers + colored reportChecks
// ---------------------------------------------------------------------------

func TestPaint(t *testing.T) {
	if paint("\x1b[31mX\x1b[0m", true) != "\x1b[31mX\x1b[0m" {
		t.Error("color on should keep escapes")
	}
	if paint("\x1b[31mX\x1b[0m", false) != "X" {
		t.Errorf("color off should strip -> %q", paint("\x1b[31mX\x1b[0m", false))
	}
}

func TestStripANSI(t *testing.T) {
	if stripANSI("\x1b[2m→\x1b[0m") != "→" {
		t.Error("strip escapes")
	}
	if stripANSI("plain") != "plain" {
		t.Error("plain unchanged")
	}
}

func TestFirstLine(t *testing.T) {
	if firstLine("a\nb") != "a" {
		t.Error("multi")
	}
	if firstLine("a") != "a" {
		t.Error("single")
	}
}

func TestPlural(t *testing.T) {
	if plural(1, "y", "ies") != "y" {
		t.Error("1")
	}
	if plural(2, "y", "ies") != "ies" {
		t.Error("2")
	}
}

func TestReportChecksColored(t *testing.T) {
	// Colored, non-json, with a failing required (hint) + optional + ok.
	results := []checkResult{
		{Name: "git", OK: true, Required: true, Detail: "installed"},
		{Name: "tmux", OK: false, Required: true, Detail: "not found on PATH", Hint: "install tmux"},
		{Name: "node", OK: false, Required: false, Detail: "not found on PATH", Hint: "install node"},
	}
	var buf bytes.Buffer
	err := reportChecks(&buf, results, false, true)
	if !errors.Is(err, wefterr.ErrDependencyMissing) {
		t.Fatalf("want dep missing, got %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("want color escapes, got %q", out)
	}
	if !strings.Contains(out, "install tmux") {
		t.Fatalf("want hint, got %q", out)
	}

	// All present, colored.
	var ok bytes.Buffer
	if err := reportChecks(&ok, []checkResult{{Name: "git", OK: true, Required: true}}, false, true); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if !strings.Contains(ok.String(), "all required dependencies present") {
		t.Fatalf("want all present, got %q", ok.String())
	}
}

func TestSymbol(t *testing.T) {
	if symbol(checkResult{OK: true}, false) == "" {
		t.Error("ok")
	}
	if symbol(checkResult{Required: true}, false) == "" {
		t.Error("required missing")
	}
	if symbol(checkResult{}, false) == "" {
		t.Error("optional missing")
	}
}
