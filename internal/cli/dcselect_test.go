package cli

import (
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

func TestDcSelect(t *testing.T) {
	t.Run("shows the n-th item", func(t *testing.T) {
		// Sorted order: oasys(1, running), oasys-ui(2, running), then stopped.
		st := dcTmuxState{
			all: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2") +
				dcPaneLine("%5", "@3", "0", oaFolder, oaConfig),
			main:      dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			mainAfter: dcPaneLine("%5", "@1", "0", oaFolder, oaConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "select", "1"); err != nil {
			t.Fatal(err)
		}
		if sp := recorded(lines, "swap-pane"); !strings.Contains(sp, "-s %5 -t %1") {
			t.Errorf("swap-pane = %q", sp)
		}
		if sw := recorded(lines, "select-window"); !strings.Contains(sw, "weft/dc:grid") {
			t.Errorf("select-window = %q", sw)
		}
		if sp := recorded(lines, "select-pane"); !strings.Contains(sp, "%5") {
			t.Errorf("select-pane = %q", sp)
		}
	})

	t.Run("stopped item is brought up", func(t *testing.T) {
		st := dcTmuxState{
			all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "select", "3"); err != nil { // gantry
			t.Fatal(err)
		}
		if up := recorded(lines, "devcontainer up"); !strings.Contains(up, "/u/gantry") {
			t.Errorf("up = %q", up)
		}
	})

	t.Run("errors", func(t *testing.T) {
		if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "select", "42"); err == nil ||
			!strings.Contains(err.Error(), "no devcontainer #42") {
			t.Fatalf("out of range: %v", err)
		}
		if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "select", "zero"); err == nil ||
			!strings.Contains(err.Error(), "wants a number") {
			t.Fatalf("non-number: %v", err)
		}
		if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "select", "0"); err == nil {
			t.Fatal("zero should error")
		}
		bad := func(c sysexec.Call) (sysexec.Result, error) {
			return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
		}
		if _, _, err := runCLI(t, bad, "", "dc", "select", "1"); err == nil {
			t.Fatal("scan error should propagate")
		}
	})

	t.Run("up failure propagates", func(t *testing.T) {
		inner := dcHandler(dcFixture(), dcTmuxState{})
		up := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "devcontainer up") {
				return sysexec.Result{Stdout: `{"outcome":"error","description":"boom"}`}, nil
			}
			return inner(c)
		}
		if _, _, err := runCLI(t, up, "", "dc", "select", "3"); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("up failure: %v", err)
		}
	})

	t.Run("show failure propagates", func(t *testing.T) {
		inner := dcHandler(dcFixture(), dcTmuxState{
			all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		})
		n := 0
		h := func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "list-panes -s") {
				n++
				if n >= 2 { // dcShow's session-wide lookup (after the scan's)
					return sysexec.Result{ExitCode: 2}, cmdErr(c, 2)
				}
			}
			return inner(c)
		}
		if _, _, err := runCLI(t, h, "", "dc", "select", "2"); err == nil {
			t.Fatal("show failure should propagate")
		}
	})
}
