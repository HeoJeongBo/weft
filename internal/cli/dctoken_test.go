package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

func TestDcToken(t *testing.T) {
	t.Run("mints and saves the token", func(t *testing.T) {
		claudeDir := stubHome(t)
		calls := captureExec(t)
		out, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "sk-ant-oat01-abc\n", "dc", "token", "oasys-ui")
		if err != nil {
			t.Fatal(err)
		}
		argv := strings.Join((*calls)[0], " ")
		for _, want := range []string{"devcontainer exec", "--workspace-folder /u/client2/holiday", "claude setup-token", "CLAUDE_CONFIG_DIR"} {
			if !strings.Contains(argv, want) {
				t.Errorf("argv missing %q: %q", want, argv)
			}
		}
		path := filepath.Join(claudeDir, "weft-oauth-token")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "sk-ant-oat01-abc\n" {
			t.Errorf("token file = %q, err %v", data, err)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("token mode = %v", info.Mode())
		}
		if !strings.Contains(out, "saved") {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("empty paste saves nothing", func(t *testing.T) {
		stubHome(t)
		captureExec(t)
		_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "\n", "dc", "token", "oasys-ui")
		if err == nil || !strings.Contains(err.Error(), "no token pasted") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("no running match", func(t *testing.T) {
		_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "token", "gantry")
		if err == nil || !strings.Contains(err.Error(), "no running devcontainer") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("ambiguous running match", func(t *testing.T) {
		out, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "token", "oasys")
		if err == nil || !strings.Contains(err.Error(), "pick one by name") {
			t.Fatalf("got %v", err)
		}
		if !strings.Contains(out, "oasys-ui") {
			t.Errorf("candidates table not shown:\n%s", out)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		bad := func(c sysexec.Call) (sysexec.Result, error) {
			return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
		}
		if _, _, err := runCLI(t, bad, "", "dc", "token"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("setup-token run failure", func(t *testing.T) {
		stubHome(t)
		restore := swapExec(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		})
		t.Cleanup(restore)
		_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "tok\n", "dc", "token", "oasys-ui")
		if err == nil {
			t.Fatal("want error from setup-token")
		}
	})

	t.Run("home lookup failure", func(t *testing.T) {
		captureExec(t)
		saved := userHomeDir
		userHomeDir = func() (string, error) { return "", fmt.Errorf("no home") }
		t.Cleanup(func() { userHomeDir = saved })
		_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "tok\n", "dc", "token", "oasys-ui")
		if err == nil || !strings.Contains(err.Error(), "no home") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("write failure", func(t *testing.T) {
		captureExec(t)
		home := t.TempDir() // no .claude dir -> WriteFile fails
		saved := userHomeDir
		userHomeDir = func() (string, error) { return home, nil }
		t.Cleanup(func() { userHomeDir = saved })
		_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "tok\n", "dc", "token", "oasys-ui")
		if err == nil {
			t.Fatal("want write error")
		}
	})
}
