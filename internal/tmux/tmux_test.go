package tmux

import (
	"context"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func newFake(h func(sysexec.Call) (sysexec.Result, error)) (*sysexec.FakeRunner, *Exec) {
	f := &sysexec.FakeRunner{Handler: h}
	return f, New(f)
}

func TestParseWindows(t *testing.T) {
	out := "@0\tmain\t0\t1\tclaude\t0\t1720000000\n" +
		"@3\tfeat-auth\t3\t0\tnode\t0\t1720000123\n" +
		"@4\tdead\t4\t0\tbash\t1\t1720000200\n"
	_, tm := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: out}, nil
	})
	ws, err := tm.ListWindows(context.Background(), "weft/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 3 {
		t.Fatalf("want 3 windows, got %d", len(ws))
	}
	if ws[0].ID != "@0" || ws[0].Name != "main" || !ws[0].Active || ws[0].PaneCommand != "claude" {
		t.Errorf("bad window[0]: %+v", ws[0])
	}
	if ws[1].Index != 3 || ws[1].Activity != 1720000123 {
		t.Errorf("bad window[1]: %+v", ws[1])
	}
	if !ws[2].PaneDead {
		t.Errorf("window[2] should be pane-dead: %+v", ws[2])
	}
}

func TestNewWindowArgsAndID(t *testing.T) {
	f, tm := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "@7\n"}, nil
	})
	id, err := tm.NewWindow(context.Background(), "weft/app", "feat-auth", "/wt",
		[]string{"devcontainer", "exec", "--workspace-folder", "/wt", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "@7" {
		t.Errorf("want id @7, got %q", id)
	}
	line := f.LastCall().Line()
	for _, want := range []string{"new-window", "-t weft/app:", "-n feat-auth", "-c /wt", "-P", "-F #{window_id}", "-- devcontainer exec"} {
		if !strings.Contains(line, want) {
			t.Errorf("argv %q missing %q", line, want)
		}
	}
}

func TestHasSession(t *testing.T) {
	// present
	_, tm := newFake(func(sysexec.Call) (sysexec.Result, error) { return sysexec.Result{}, nil })
	if ok, err := tm.HasSession(context.Background(), "weft/app"); err != nil || !ok {
		t.Fatalf("want present, got ok=%v err=%v", ok, err)
	}
	// absent (exit 1)
	_, tm = newFake(func(c sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{ExitCode: 1}, &wefterr.CmdError{Cmd: "tmux", Args: c.Args, ExitCode: 1}
	})
	if ok, err := tm.HasSession(context.Background(), "weft/app"); err != nil || ok {
		t.Fatalf("want absent, got ok=%v err=%v", ok, err)
	}
	// missing binary (exit -1) -> propagate
	_, tm = newFake(func(c sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{ExitCode: -1}, &wefterr.CmdError{Cmd: "tmux", Args: c.Args, ExitCode: -1}
	})
	if _, err := tm.HasSession(context.Background(), "weft/app"); err == nil {
		t.Fatal("want error to propagate for missing binary")
	}
}

func TestListWindowsAbsentSession(t *testing.T) {
	_, tm := newFake(func(c sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{ExitCode: 1}, &wefterr.CmdError{Cmd: "tmux", Args: c.Args, ExitCode: 1}
	})
	ws, err := tm.ListWindows(context.Background(), "weft/none")
	if err != nil || ws != nil {
		t.Fatalf("want empty+nil, got ws=%v err=%v", ws, err)
	}
}

func TestNewSessionArgs(t *testing.T) {
	f, tm := newFake(nil)
	if err := tm.NewSession(context.Background(), "weft/app", "/repo"); err != nil {
		t.Fatal(err)
	}
	line := f.LastCall().Line()
	for _, want := range []string{"new-session", "-d", "-s weft/app", "-c /repo"} {
		if !strings.Contains(line, want) {
			t.Errorf("argv %q missing %q", line, want)
		}
	}
}
