package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/tui"
)

func dcPsLine(name, state, folder, config string) string {
	return fmt.Sprintf(`{"ID":"%s-id","Names":"%s","Image":"img","State":"%s","Status":"x","Labels":"devcontainer.local_folder=%s,devcontainer.config_file=%s"}`,
		name, name, state, folder, config) + "\n"
}

// dcFixture: a stopped leftover BEFORE and AFTER the running oasys-ui (dedup
// both ways), a second running devcontainer, and a stopped root-style one.
func dcFixture() string {
	uiFolder, uiConfig := "/u/client2/holiday", "/u/client2/holiday/.devcontainer/oasys-ui/devcontainer.json"
	return dcPsLine("oasys-ui-old", "exited", uiFolder, uiConfig) +
		dcPsLine("oasys-ui-dev-1", "running", uiFolder, uiConfig) +
		dcPsLine("oasys-ui-older", "exited", uiFolder, uiConfig) +
		dcPsLine("oasys-dev-1", "running", "/u/client/holiday", "/u/client/holiday/.devcontainer/oasys/devcontainer.json") +
		dcPsLine("gantry_devcontainer-dev-1", "exited", "/u/gantry", "/u/gantry/.devcontainer/devcontainer.json")
}

func dcHandler(psOut string) func(sysexec.Call) (sysexec.Result, error) {
	return func(c sysexec.Call) (sysexec.Result, error) {
		line := c.Line()
		switch {
		case strings.Contains(line, "docker ps"):
			return sysexec.Result{Stdout: psOut}, nil
		case strings.Contains(line, "devcontainer up"):
			return sysexec.Result{Stdout: "building...\n" + `{"outcome":"success","containerId":"c1","remoteUser":"u"}`}, nil
		}
		return sysexec.Result{}, nil
	}
}

func swapPicker(f func(context.Context, []tui.DcItem) (int, error)) func() {
	s := runDcPicker
	runDcPicker = f
	return func() { runDcPicker = s }
}

// captureExec stubs the interactive attach and records its argv.
func captureExec(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	restore := swapExec(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return exec.CommandContext(ctx, "true")
	})
	t.Cleanup(restore)
	return &calls
}

func TestDcListNonTTY(t *testing.T) {
	out, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"STATE", "WORKSPACE", "oasys-ui", "oasys-ui-dev-1", "gantry", "/u/gantry"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "oasys-ui-old") {
		t.Errorf("stopped duplicate not deduped:\n%s", out)
	}
	// running first (name asc), stopped last.
	if oi, ui, gi := strings.Index(out, "oasys-dev-1"), strings.Index(out, "oasys-ui-dev-1"), strings.Index(out, "gantry"); oi >= ui || ui >= gi {
		t.Errorf("order wrong (oasys=%d oasys-ui=%d gantry=%d):\n%s", oi, ui, gi, out)
	}
}

func TestDcQueryDryRun(t *testing.T) {
	out, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "oasys-ui", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"devcontainer exec --workspace-folder /u/client2/holiday",
		"--config /u/client2/holiday/.devcontainer/oasys-ui/devcontainer.json",
		"exec zsh -l",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run argv missing %q:\n%s", want, out)
		}
	}
}

func TestDcRunCommand(t *testing.T) {
	calls := captureExec(t)
	_, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "oasys-ui", "--", "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join((*calls)[0], " ")
	if !strings.Contains(got, "devcontainer exec --workspace-folder /u/client2/holiday") || !strings.HasSuffix(got, "echo hi") {
		t.Errorf("exec argv = %q", got)
	}
}

func TestDcStoppedNeedsStart(t *testing.T) {
	_, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "gantry")
	if err == nil || !strings.Contains(err.Error(), "--start") {
		t.Fatalf("want --start hint, got %v", err)
	}
}

func TestDcStart(t *testing.T) {
	calls := captureExec(t)
	_, stderr, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "gantry", "--start", "-v")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "devcontainer up (gantry)") || !strings.Contains(stderr, "building...") {
		t.Errorf("stderr missing up progress:\n%s", stderr)
	}
	if got := strings.Join((*calls)[0], " "); !strings.Contains(got, "--workspace-folder /u/gantry") {
		t.Errorf("exec argv = %q", got)
	}
}

func TestDcStartUpFails(t *testing.T) {
	h := func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), "devcontainer up") {
			return sysexec.Result{Stdout: `{"outcome":"error","description":"boom"}`}, nil
		}
		return dcHandler(dcFixture())(c)
	}
	_, _, err := runCLI(t, h, "", "dc", "gantry", "--start")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want up failure, got %v", err)
	}
}

func TestDcErrors(t *testing.T) {
	t.Run("no match", func(t *testing.T) {
		_, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "zzz")
		if err == nil || !strings.Contains(err.Error(), `no devcontainer matches "zzz"`) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("none found", func(t *testing.T) {
		_, _, err := runCLI(t, dcHandler(""), "", "dc")
		if err == nil || !strings.Contains(err.Error(), "no devcontainers found") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("docker error", func(t *testing.T) {
		h := func(c sysexec.Call) (sysexec.Result, error) {
			return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
		}
		if _, _, err := runCLI(t, h, "", "dc"); err == nil {
			t.Fatal("want docker error")
		}
	})
	t.Run("two queries", func(t *testing.T) {
		_, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "a", "b")
		if err == nil || !strings.Contains(err.Error(), "at most one query") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("ambiguous non-tty", func(t *testing.T) {
		out, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "oasys", "--", "echo", "hi")
		if err == nil || !strings.Contains(err.Error(), "2 devcontainers match") {
			t.Fatalf("got %v", err)
		}
		if !strings.Contains(out, "oasys-ui") {
			t.Errorf("candidates table not shown:\n%s", out)
		}
	})
}

func TestDcNonTTYSingleWithStart(t *testing.T) {
	calls := captureExec(t)
	single := dcPsLine("solo-dev-1", "running", "/u/solo", "/u/solo/.devcontainer/devcontainer.json")
	_, _, err := runCLI(t, dcHandler(single), "", "dc", "--start")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join((*calls)[0], " "); !strings.Contains(got, "--workspace-folder /u/solo") {
		t.Errorf("exec argv = %q", got)
	}
}

func TestDcPickerFlow(t *testing.T) {
	tty := func() func() { return swapTTY(func(io.Writer) bool { return true }) }

	t.Run("select stopped auto-starts", func(t *testing.T) {
		defer tty()()
		var items []tui.DcItem
		defer swapPicker(func(_ context.Context, in []tui.DcItem) (int, error) {
			items = in
			return 2, nil // gantry (stopped) — sorted after the two running ones
		})()
		calls := captureExec(t)
		_, stderr, err := runCLI(t, dcHandler(dcFixture()), "", "dc")
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 {
			t.Errorf("picker got %d items", len(items))
		}
		if !strings.Contains(stderr, "devcontainer up (gantry)") {
			t.Errorf("auto-start missing:\n%s", stderr)
		}
		if got := strings.Join((*calls)[0], " "); !strings.Contains(got, "--workspace-folder /u/gantry") {
			t.Errorf("exec argv = %q", got)
		}
	})

	t.Run("query narrows picker", func(t *testing.T) {
		defer tty()()
		var items []tui.DcItem
		defer swapPicker(func(_ context.Context, in []tui.DcItem) (int, error) {
			items = in
			return tui.DcCancelled, nil
		})()
		if _, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc", "oasys"); err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Errorf("picker got %d items, want the 2 oasys matches", len(items))
		}
	})

	t.Run("cancel does nothing", func(t *testing.T) {
		defer tty()()
		defer swapPicker(func(context.Context, []tui.DcItem) (int, error) { return tui.DcCancelled, nil })()
		calls := captureExec(t)
		if _, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc"); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 0 {
			t.Errorf("attach ran despite cancel: %v", *calls)
		}
	})

	t.Run("rescan loops", func(t *testing.T) {
		defer tty()()
		n := 0
		defer swapPicker(func(context.Context, []tui.DcItem) (int, error) {
			n++
			if n == 1 {
				return tui.DcRescan, nil
			}
			return tui.DcCancelled, nil
		})()
		if _, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc"); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Errorf("picker called %d times, want 2", n)
		}
	})

	t.Run("picker error propagates", func(t *testing.T) {
		defer tty()()
		defer swapPicker(func(context.Context, []tui.DcItem) (int, error) {
			return tui.DcCancelled, fmt.Errorf("tea broke")
		})()
		if _, _, err := runCLI(t, dcHandler(dcFixture()), "", "dc"); err == nil || !strings.Contains(err.Error(), "tea broke") {
			t.Fatalf("got %v", err)
		}
	})
}
