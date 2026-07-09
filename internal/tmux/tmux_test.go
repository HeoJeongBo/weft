package tmux

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// errBoom is a generic non-CmdError failure used to exercise error propagation.
var errBoom = errors.New("boom")

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

func TestNewSessionNoStartDir(t *testing.T) {
	f, tm := newFake(nil)
	if err := tm.NewSession(context.Background(), "weft/app", ""); err != nil {
		t.Fatal(err)
	}
	line := f.LastCall().Line()
	if strings.Contains(line, "-c") {
		t.Errorf("empty startDir should omit -c: %q", line)
	}
	for _, want := range []string{"new-session", "-d", "-s weft/app"} {
		if !strings.Contains(line, want) {
			t.Errorf("argv %q missing %q", line, want)
		}
	}
}

func TestInTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	if !InTmux() {
		t.Error("want InTmux true when TMUX is set")
	}
	t.Setenv("TMUX", "")
	if InTmux() {
		t.Error("want InTmux false when TMUX is empty")
	}
}

func TestAttachArgs(t *testing.T) {
	got := AttachArgs("weft/app")
	want := []string{"attach-session", "-t", "weft/app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AttachArgs = %v, want %v", got, want)
	}
}

// TestSimpleMutations covers every one-shot mutate wrapper: success argv shape
// plus error propagation.
func TestSimpleMutations(t *testing.T) {
	tests := []struct {
		name         string
		call         func(*Exec) error
		wantContains []string
	}{
		{
			"kill-window", func(e *Exec) error { return e.KillWindow(context.Background(), "@3") },
			[]string{"kill-window", "-t @3"},
		},
		{
			"kill-session", func(e *Exec) error { return e.KillSession(context.Background(), "weft/app") },
			[]string{"kill-session", "-t weft/app"},
		},
		{
			"select-window", func(e *Exec) error { return e.SelectWindow(context.Background(), "weft/app:1") },
			[]string{"select-window", "-t weft/app:1"},
		},
		{
			"switch-client", func(e *Exec) error { return e.SwitchClient(context.Background(), "weft/app:1") },
			[]string{"switch-client", "-t weft/app:1"},
		},
		{
			"send-keys", func(e *Exec) error { return e.SendKeys(context.Background(), "@3", "C-c", "Enter") },
			[]string{"send-keys", "-t @3", "C-c", "Enter"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// success: assert argv and that it is a mutate.
			f, tm := newFake(nil)
			if err := tt.call(tm); err != nil {
				t.Fatal(err)
			}
			got := f.LastCall()
			if got.Kind != "mutate" {
				t.Errorf("%s should Mutate, got %q", tt.name, got.Kind)
			}
			line := got.Line()
			for _, want := range tt.wantContains {
				if !strings.Contains(line, want) {
					t.Errorf("argv %q missing %q", line, want)
				}
			}

			// error propagates.
			_, tm = newFake(func(sysexec.Call) (sysexec.Result, error) {
				return sysexec.Result{}, errBoom
			})
			if err := tt.call(tm); err == nil {
				t.Errorf("%s should propagate error", tt.name)
			}
		})
	}
}

func TestNewWindowError(t *testing.T) {
	_, tm := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errBoom
	})
	id, err := tm.NewWindow(context.Background(), "weft/app", "feat", "", nil)
	if err == nil || id != "" {
		t.Fatalf("want error+empty id, got id=%q err=%v", id, err)
	}
}

func TestListWindowsError(t *testing.T) {
	// exit 2 (not the "absent session" sentinel of exit 1) propagates.
	_, tm := newFake(func(c sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{ExitCode: 2}, &wefterr.CmdError{Cmd: "tmux", Args: c.Args, ExitCode: 2}
	})
	if ws, err := tm.ListWindows(context.Background(), "weft/app"); err == nil || ws != nil {
		t.Fatalf("want error+nil for exit 2, got ws=%v err=%v", ws, err)
	}
}

func TestParseWindowsShortLine(t *testing.T) {
	// A line with fewer than 7 tab-separated fields is skipped; a blank line too.
	out := "@0\tmain\t0\t1\tclaude\t0\t1720000000\n" +
		"@1\tbroken\t1\t0\tnode\n" + // only 5 fields -> skipped
		"\n" +
		"@2\tok\t2\t0\tbash\t0\t1720000500\n"
	_, tm := newFake(func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: out}, nil
	})
	ws, err := tm.ListWindows(context.Background(), "weft/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 {
		t.Fatalf("want 2 windows (short line skipped), got %d: %+v", len(ws), ws)
	}
	if ws[0].ID != "@0" || ws[1].ID != "@2" {
		t.Errorf("unexpected windows: %+v", ws)
	}
}
