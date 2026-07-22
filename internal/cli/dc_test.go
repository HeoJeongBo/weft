package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/tui"
)

func dcPsLine(name, state, folder, config string) string {
	return fmt.Sprintf(`{"ID":"%s-id","Names":"%s","Image":"img","State":"%s","Status":"x","Labels":"devcontainer.local_folder=%s,devcontainer.config_file=%s"}`,
		name, name, state, folder, config) + "\n"
}

// dcPaneLine mimics a list-panes entry for a pane weft started for the given
// devcontainer.
func dcPaneLine(id, window, dead, folder, config string) string {
	return fmt.Sprintf("%s\t%s\t%s\tnode\tdevcontainer exec --workspace-folder %s --config %s sh -lc x\n",
		id, window, dead, folder, config)
}

// dcSidebarLine mimics the sidebar pane's list-panes entry.
func dcSidebarLine(id string) string {
	return id + "\t@1\t0\tweft\t/opt/homebrew/bin/weft dc sidebar\n"
}

const (
	uiFolder = "/u/client2/holiday"
	uiConfig = "/u/client2/holiday/.devcontainer/oasys-ui/devcontainer.json"
	oaFolder = "/u/client/holiday"
	oaConfig = "/u/client/holiday/.devcontainer/oasys/devcontainer.json"
)

// dcFixture: a stopped leftover BEFORE and AFTER the running oasys-ui (dedup
// both ways), a second running devcontainer, and a stopped root-style one.
func dcFixture() string {
	return dcPsLine("oasys-ui-old", "exited", uiFolder, uiConfig) +
		dcPsLine("oasys-ui-dev-1", "running", uiFolder, uiConfig) +
		dcPsLine("oasys-ui-older", "exited", uiFolder, uiConfig) +
		dcPsLine("oasys-dev-1", "running", oaFolder, oaConfig) +
		dcPsLine("gantry_devcontainer-dev-1", "exited", "/u/gantry", "/u/gantry/.devcontainer/devcontainer.json")
}

// dcTmuxState drives the fake tmux: all = session-wide panes, main = the main
// window's panes before any mutation, mainAfter = what re-listing the main
// window returns after a mutation was seen ("" = same as main). Empty strings
// for all/main mean "absent" (tmux exits 1).
type dcTmuxState struct {
	all, main, mainAfter string
}

func dcHandler(psOut string, st dcTmuxState) func(sysexec.Call) (sysexec.Result, error) {
	var mu sync.Mutex
	mutated := false
	return func(c sysexec.Call) (sysexec.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		line := c.Line()
		switch {
		case strings.Contains(line, "docker ps"):
			return sysexec.Result{Stdout: psOut}, nil
		case strings.Contains(line, "devcontainer up"):
			return sysexec.Result{Stdout: "building...\n" + `{"outcome":"success","containerId":"c1","remoteUser":"u"}`}, nil
		case strings.Contains(line, "has-session"):
			return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
		case strings.Contains(line, "list-panes -s"):
			if st.all == "" {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return sysexec.Result{Stdout: st.all + "junk\nshort\tline\n"}, nil
		case strings.Contains(line, "list-panes"):
			out := st.main
			if mutated && st.mainAfter != "" {
				out = st.mainAfter
			}
			if out == "" {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return sysexec.Result{Stdout: out}, nil
		case strings.Contains(line, "new-window"), strings.Contains(line, "split-window"):
			mutated = true
			return sysexec.Result{Stdout: "%9\n"}, nil
		case strings.Contains(line, "swap-pane"), strings.Contains(line, "join-pane"),
			strings.Contains(line, "rename-window"), strings.Contains(line, "break-pane"):
			mutated = true
			return sysexec.Result{}, nil
		}
		return sysexec.Result{}, nil
	}
}

// recording wraps a handler and collects each call line.
func recording(h func(sysexec.Call) (sysexec.Result, error)) (func(sysexec.Call) (sysexec.Result, error), *[]string) {
	var mu sync.Mutex
	var lines []string
	return func(c sysexec.Call) (sysexec.Result, error) {
		mu.Lock()
		lines = append(lines, c.Line())
		mu.Unlock()
		return h(c)
	}, &lines
}

func recorded(lines *[]string, substr string) string {
	for _, l := range *lines {
		if strings.Contains(l, substr) {
			return l
		}
	}
	return ""
}

func swapPicker(f func(context.Context, []tui.DcItem) (int, error)) func() {
	s := runDcPicker
	runDcPicker = f
	return func() { runDcPicker = s }
}

// captureExec stubs the terminal handoff and records its argv.
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
	st := dcTmuxState{
		all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcPaneLine("%3", "@2", "1", oaFolder, oaConfig),
		main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig),
	}
	out, _, err := runCLI(t, dcHandler(dcFixture(), st), "", "dc")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"STATE", "WORKSPACE", "oasys-ui*", "oasys*", "gantry", "/u/gantry"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "oasys-ui-old") {
		t.Errorf("stopped duplicate not deduped:\n%s", out)
	}
	if strings.Contains(out, "gantry*") {
		t.Errorf("gantry wrongly marked attached:\n%s", out)
	}
}

func TestDcCandidateFlags(t *testing.T) {
	st := dcTmuxState{}
	_ = st
	all := []struct{ id, win, dead, folder, config string }{}
	_ = all
	// via dcScan-independent unit: build panes directly through fixtures.
	cands := matchDc(func() []dcCandidate {
		h := dcHandler(dcFixture(), dcTmuxState{
			all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcPaneLine("%3", "@2", "1", oaFolder, oaConfig),
			main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig),
		})
		fake := &sysexec.FakeRunner{Handler: h}
		cs, err := dcScan(context.Background(), fake)
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}(), "oasys")
	var ui, oa dcCandidate
	for _, c := range cands {
		switch c.Name {
		case "oasys-ui":
			ui = c
		case "oasys":
			oa = c
		}
	}
	if !ui.HasWindow || !ui.Shown || ui.PaneDead {
		t.Errorf("ui flags = %+v", ui)
	}
	if !oa.HasWindow || oa.Shown || !oa.PaneDead {
		t.Errorf("oasys flags = %+v", oa)
	}
}

func TestDcDryRun(t *testing.T) {
	t.Run("claude chain", func(t *testing.T) {
		out, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "oasys-ui", "--dry-run")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"devcontainer exec --workspace-folder /u/client2/holiday",
			"--config " + uiConfig,
			`PATH="$HOME/.local/bin:$PATH"`,
			`CLAUDE_CONFIG_DIR="$HOME/.claude"`,
			"weft-oauth-token",
			"CLAUDE_CODE_OAUTH_TOKEN",
			"command -v claude",
			"sudo -n chown",
			"curl -fsSL https://claude.ai/install.sh",
			"claude --continue || claude",
			"exec zsh -l",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("dry-run argv missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("one-shot command", func(t *testing.T) {
		out, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "oasys-ui", "--dry-run", "--", "echo", "hi")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(strings.TrimSpace(out), "echo hi") || strings.Contains(out, "claude") {
			t.Errorf("one-shot dry-run argv wrong:\n%s", out)
		}
	})
	t.Run("shell chain", func(t *testing.T) {
		out, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "oasys-ui", "--shell", "--dry-run")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "claude") || !strings.Contains(out, "exec zsh -l") {
			t.Errorf("shell dry-run argv wrong:\n%s", out)
		}
	})
}

func TestDcRunCommand(t *testing.T) {
	calls := captureExec(t)
	_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "oasys-ui", "--", "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join((*calls)[0], " ")
	if !strings.Contains(got, "devcontainer exec --workspace-folder /u/client2/holiday") || !strings.HasSuffix(got, "echo hi") {
		t.Errorf("exec argv = %q", got)
	}
}

func TestDcShowFlows(t *testing.T) {
	t.Setenv("TMUX", "")

	t.Run("first attach creates main window and sidebar", func(t *testing.T) {
		calls := captureExec(t)
		h, lines := recording(dcHandler(dcFixture(), dcTmuxState{}))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		nw := recorded(lines, "new-window")
		for _, want := range []string{"-n grid", "claude --continue || claude", "--workspace-folder /u/client2/holiday"} {
			if !strings.Contains(nw, want) {
				t.Errorf("new-window missing %q: %q", want, nw)
			}
		}
		if sb := recorded(lines, "split-window -hbf"); !strings.Contains(sb, "dc sidebar") {
			t.Errorf("sidebar split = %q", sb)
		}
		if so := recorded(lines, "set-option"); !strings.Contains(so, "-s set-clipboard on") {
			t.Errorf("set-clipboard not enabled: %q", so)
		}
		if sl := recorded(lines, "status-left "); !strings.Contains(sl, "client_prefix") {
			t.Errorf("prefix badge not set: %q", sl)
		}
		if bs := recorded(lines, "pane-active-border-style"); !strings.Contains(bs, "-w -t weft/dc:grid") {
			t.Errorf("active border style = %q", bs)
		}
		if rz := recorded(lines, "resize-pane"); !strings.Contains(rz, "-x 30") {
			t.Errorf("sidebar width not pinned: %q", rz)
		}
		if got := strings.Join((*calls)[0], " "); got != "tmux attach-session -t weft/dc" {
			t.Errorf("attach argv = %q", got)
		}
	})

	t.Run("no-sidebar skips the sidebar pane", func(t *testing.T) {
		captureExec(t)
		h, lines := recording(dcHandler(dcFixture(), dcTmuxState{}))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui", "--no-sidebar"); err != nil {
			t.Fatal(err)
		}
		if sb := recorded(lines, "split-window -hbf"); sb != "" {
			t.Errorf("sidebar split despite --no-sidebar: %q", sb)
		}
	})

	t.Run("new devcontainer parks the shown one via swap", func(t *testing.T) {
		captureExec(t)
		st := dcTmuxState{
			all:       dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			main:      dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			mainAfter: dcPaneLine("%9", "@1", "0", "/u/gantry", "/u/gantry/.devcontainer/devcontainer.json") + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "gantry", "--start"); err != nil {
			t.Fatal(err)
		}
		if nw := recorded(lines, "new-window -d"); !strings.Contains(nw, "-n dc-gantry") || !strings.Contains(nw, "--workspace-folder /u/gantry") {
			t.Errorf("background window = %q", nw)
		}
		if sp := recorded(lines, "swap-pane"); !strings.Contains(sp, "-s %9 -t %1") {
			t.Errorf("swap-pane = %q", sp)
		}
	})

	t.Run("parked pane swaps into view", func(t *testing.T) {
		captureExec(t)
		st := dcTmuxState{
			all: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2") +
				dcPaneLine("%5", "@3", "0", oaFolder, oaConfig),
			main:      dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			mainAfter: dcPaneLine("%5", "@1", "0", oaFolder, oaConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-dev-1"); err != nil {
			t.Fatal(err)
		}
		if sp := recorded(lines, "swap-pane"); !strings.Contains(sp, "-s %5 -t %1") {
			t.Errorf("swap-pane = %q", sp)
		}
		for _, forbidden := range []string{"new-window", "split-window -h -t", "join-pane"} {
			if l := recorded(lines, forbidden); l != "" {
				t.Errorf("unexpected %s: %q", forbidden, l)
			}
		}
	})

	t.Run("already shown is a no-op focus", func(t *testing.T) {
		captureExec(t)
		st := dcTmuxState{
			all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"swap-pane", "new-window", "split-window -h -t", "join-pane", "break-pane"} {
			if l := recorded(lines, forbidden); l != "" {
				t.Errorf("unexpected %s: %q", forbidden, l)
			}
		}
		if l := recorded(lines, "select-pane"); !strings.Contains(l, "%1") {
			t.Errorf("select-pane = %q", l)
		}
	})

	t.Run("legacy window is promoted to main", func(t *testing.T) {
		captureExec(t)
		st := dcTmuxState{
			all:       dcPaneLine("%7", "@4", "0", uiFolder, uiConfig),
			mainAfter: dcPaneLine("%7", "@4", "0", uiFolder, uiConfig),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		if rn := recorded(lines, "rename-window"); !strings.Contains(rn, "-t %7 grid") {
			t.Errorf("rename-window = %q", rn)
		}
		if bp := recorded(lines, "break-pane"); bp != "" {
			t.Errorf("unexpected break-pane for lone pane: %q", bp)
		}
	})

	t.Run("crowded legacy pane is carved out before promotion", func(t *testing.T) {
		captureExec(t)
		st := dcTmuxState{
			all: dcPaneLine("%7", "@4", "0", uiFolder, uiConfig) +
				dcPaneLine("%8", "@4", "0", oaFolder, oaConfig),
			mainAfter: dcPaneLine("%7", "@4", "0", uiFolder, uiConfig),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		if bp := recorded(lines, "break-pane"); !strings.Contains(bp, "-s %7") {
			t.Errorf("break-pane = %q", bp)
		}
		if rn := recorded(lines, "rename-window"); !strings.Contains(rn, "-t %7 grid") {
			t.Errorf("rename-window = %q", rn)
		}
	})

	t.Run("parked pane joins an empty main", func(t *testing.T) {
		captureExec(t)
		st := dcTmuxState{
			all:       dcSidebarLine("%2") + dcPaneLine("%5", "@3", "0", uiFolder, uiConfig),
			main:      dcSidebarLine("%2"),
			mainAfter: dcPaneLine("%5", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		if jp := recorded(lines, "join-pane"); !strings.Contains(jp, "-h -s %5 -t weft/dc:grid") {
			t.Errorf("join-pane = %q", jp)
		}
	})

	t.Run("fresh pane splits right of the sidebar", func(t *testing.T) {
		captureExec(t)
		st := dcTmuxState{
			all:       dcSidebarLine("%2"),
			main:      dcSidebarLine("%2"),
			mainAfter: dcPaneLine("%9", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		if sw := recorded(lines, "split-window -h -t"); !strings.Contains(sw, "--workspace-folder /u/client2/holiday") {
			t.Errorf("split-window = %q", sw)
		}
	})

	t.Run("grid leftovers are parked", func(t *testing.T) {
		captureExec(t)
		// The target is the SECOND claude pane in the main window: the first
		// one (%3) is the shown slot and must be parked instead.
		st := dcTmuxState{
			all: dcPaneLine("%3", "@1", "0", oaFolder, oaConfig) +
				dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			main: dcPaneLine("%3", "@1", "0", oaFolder, oaConfig) +
				dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		if bp := recorded(lines, "break-pane"); !strings.Contains(bp, "-s %3") {
			t.Errorf("break-pane = %q", bp)
		}
		if sp := recorded(lines, "swap-pane"); sp != "" {
			t.Errorf("unexpected swap-pane: %q", sp)
		}
	})

	t.Run("inside tmux switches client", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1/default,123,0")
		calls := captureExec(t)
		st := dcTmuxState{
			all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 0 {
			t.Errorf("attach exec ran inside tmux: %v", *calls)
		}
		if l := recorded(lines, "switch-client"); !strings.Contains(l, "weft/dc:grid") {
			t.Errorf("switch-client = %q", l)
		}
	})
}

func TestDcStoppedNeedsStart(t *testing.T) {
	_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "gantry")
	if err == nil || !strings.Contains(err.Error(), "--start") {
		t.Fatalf("want --start hint, got %v", err)
	}
}

func TestDcStartUpFails(t *testing.T) {
	h := func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), "devcontainer up") {
			return sysexec.Result{Stdout: `{"outcome":"error","description":"boom"}`}, nil
		}
		return dcHandler(dcFixture(), dcTmuxState{})(c)
	}
	_, _, err := runCLI(t, h, "", "dc", "gantry", "--start")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want up failure, got %v", err)
	}
}

func TestDcStartStreamsAtVerbose(t *testing.T) {
	t.Setenv("TMUX", "")
	captureExec(t)
	_, stderr, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "gantry", "--start", "-v")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "devcontainer up (gantry)") || !strings.Contains(stderr, "building...") {
		t.Errorf("stderr missing streamed up progress:\n%s", stderr)
	}
}

func TestDcTmuxErrors(t *testing.T) {
	t.Setenv("TMUX", "")
	base := dcTmuxState{
		all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
	}
	failOn := func(st dcTmuxState, match string, code int) func(sysexec.Call) (sysexec.Result, error) {
		inner := dcHandler(dcFixture(), st)
		return func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), match) {
				return sysexec.Result{ExitCode: code}, cmdErr(c, code)
			}
			return inner(c)
		}
	}
	for _, tc := range []struct {
		name, query, match string
		code               int
		st                 dcTmuxState
	}{
		{"has-session hard failure", "oasys-ui", "has-session", 2, base},
		{"new-session failure", "oasys-ui", "new-session", 1, base},
		{"list-panes -s hard failure", "oasys-ui", "list-panes -s", 2, base},
		{"new-window failure", "oasys-ui", "new-window", 1, dcTmuxState{}},
		{"swap-pane failure", "oasys-dev-1", "swap-pane", 1, dcTmuxState{
			all:  base.all + dcPaneLine("%5", "@3", "0", oaFolder, oaConfig),
			main: base.main,
		}},
		{"background window failure", "gantry", "new-window", 1, base},
		{"split-right failure", "oasys-ui", "split-window -h -t", 1, dcTmuxState{all: dcSidebarLine("%2"), main: dcSidebarLine("%2")}},
		{"join failure", "oasys-ui", "join-pane", 1, dcTmuxState{all: dcSidebarLine("%2") + dcPaneLine("%5", "@3", "0", uiFolder, uiConfig), main: dcSidebarLine("%2")}},
		{"rename failure", "oasys-ui", "rename-window", 1, dcTmuxState{all: dcPaneLine("%7", "@4", "0", uiFolder, uiConfig)}},
		{"promote break failure", "oasys-ui", "break-pane", 1, dcTmuxState{
			all: dcPaneLine("%7", "@4", "0", uiFolder, uiConfig) + dcPaneLine("%8", "@4", "0", oaFolder, oaConfig),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captureExec(t)
			args := []string{"dc", tc.query}
			if tc.query == "gantry" {
				args = append(args, "--start")
			}
			if _, _, err := runCLI(t, failOn(tc.st, tc.match, tc.code), "", args...); err == nil {
				t.Fatal("want error")
			}
		})
	}

	t.Run("main lookup fails", func(t *testing.T) {
		captureExec(t)
		n := 0
		inner := dcHandler(dcFixture(), dcTmuxState{})
		h := func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			if strings.Contains(line, "list-panes") && !strings.Contains(line, "list-panes -s") {
				n++
				if n >= 2 { // the lookup inside dcShow (after the scan probe)
					return sysexec.Result{ExitCode: 2}, cmdErr(c, 2)
				}
			}
			return inner(c)
		}
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("re-list after mutation fails", func(t *testing.T) {
		captureExec(t)
		n := 0
		inner := dcHandler(dcFixture(), dcTmuxState{})
		h := func(c sysexec.Call) (sysexec.Result, error) {
			line := c.Line()
			if strings.Contains(line, "list-panes") && !strings.Contains(line, "list-panes -s") {
				n++
				if n >= 3 { // scan probe, dcShow lookup, then the post-mutation re-list
					return sysexec.Result{ExitCode: 2}, cmdErr(c, 2)
				}
			}
			return inner(c)
		}
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("executable lookup failure skips sidebar", func(t *testing.T) {
		captureExec(t)
		saved := executablePath
		executablePath = func() (string, error) { return "", fmt.Errorf("nope") }
		t.Cleanup(func() { executablePath = saved })
		h, lines := recording(dcHandler(dcFixture(), dcTmuxState{}))
		if _, _, err := runCLI(t, h, "", "dc", "oasys-ui"); err != nil {
			t.Fatal(err)
		}
		if sb := recorded(lines, "split-window -hbf"); sb != "" {
			t.Errorf("sidebar split despite exe error: %q", sb)
		}
	})
}

func TestDcErrors(t *testing.T) {
	t.Run("no match", func(t *testing.T) {
		_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "zzz")
		if err == nil || !strings.Contains(err.Error(), `no devcontainer matches "zzz"`) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("none found", func(t *testing.T) {
		_, _, err := runCLI(t, dcHandler("", dcTmuxState{}), "", "dc")
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
		_, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "a", "b")
		if err == nil || !strings.Contains(err.Error(), "at most one query") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("ambiguous non-tty", func(t *testing.T) {
		out, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "oasys", "--", "echo", "hi")
		if err == nil || !strings.Contains(err.Error(), "2 devcontainers match") {
			t.Fatalf("got %v", err)
		}
		if !strings.Contains(out, "oasys-ui") {
			t.Errorf("candidates table not shown:\n%s", out)
		}
	})
}

func TestDcNonTTYSingleWithStart(t *testing.T) {
	t.Setenv("TMUX", "")
	calls := captureExec(t)
	single := dcPsLine("solo-dev-1", "running", "/u/solo", "/u/solo/.devcontainer/devcontainer.json")
	_, _, err := runCLI(t, dcHandler(single, dcTmuxState{}), "", "dc", "--start")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join((*calls)[0], " "); got != "tmux attach-session -t weft/dc" {
		t.Errorf("attach argv = %q", got)
	}
}

func TestDcPickerFlow(t *testing.T) {
	tty := func() func() { return swapTTY(func(io.Writer) bool { return true }) }

	t.Run("select stopped auto-starts", func(t *testing.T) {
		t.Setenv("TMUX", "")
		defer tty()()
		var items []tui.DcItem
		defer swapPicker(func(_ context.Context, in []tui.DcItem) (int, error) {
			items = in
			return 2, nil // gantry (stopped) — sorted after the two running ones
		})()
		captureExec(t)
		st := dcTmuxState{
			all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
			main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		}
		h, lines := recording(dcHandler(dcFixture(), st))
		_, stderr, err := runCLI(t, h, "", "dc")
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 || !items[1].HasWindow || items[2].HasWindow {
			t.Errorf("items = %+v", items)
		}
		if !strings.Contains(stderr, "devcontainer up (gantry)") {
			t.Errorf("auto-start missing:\n%s", stderr)
		}
		if nw := recorded(lines, "new-window -d"); !strings.Contains(nw, "--workspace-folder /u/gantry") {
			t.Errorf("background window = %q", nw)
		}
	})

	t.Run("query narrows picker", func(t *testing.T) {
		defer tty()()
		var items []tui.DcItem
		defer swapPicker(func(_ context.Context, in []tui.DcItem) (int, error) {
			items = in
			return tui.DcCancelled, nil
		})()
		if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "oasys"); err != nil {
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
		if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc"); err != nil {
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
		if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc"); err != nil {
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
		if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc"); err == nil || !strings.Contains(err.Error(), "tea broke") {
			t.Fatalf("got %v", err)
		}
	})
}
