package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

func sbKey(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
	}
}

func sbModel(t *testing.T, st dcTmuxState) sidebarModel {
	t.Helper()
	fake := &sysexec.FakeRunner{Handler: dcHandler(dcFixture(), st)}
	return newSidebarModel(context.Background(), fake)
}

func sbUpdate(t *testing.T, m sidebarModel, msg tea.Msg) (sidebarModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(sidebarModel), cmd
}

// stubHome points userHomeDir at a temp dir and returns its .claude path.
func stubHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = saved })
	return filepath.Join(home, ".claude")
}

func TestSidebarScanAndNavigation(t *testing.T) {
	m := sbModel(t, dcTmuxState{})

	// Init issues scan + usage; run the scan cmd to get real items.
	scan := m.scanCmd()()
	m, cmd := sbUpdate(t, m, scan)
	if len(m.items) != 3 || cmd == nil {
		t.Fatalf("items = %d", len(m.items))
	}
	m, _ = sbUpdate(t, m, sbKey("down"))
	m, _ = sbUpdate(t, m, sbKey("j"))
	if m.cursor != 2 {
		t.Errorf("cursor = %d", m.cursor)
	}
	m, _ = sbUpdate(t, m, sbKey("j")) // clamp at bottom
	if m.cursor != 2 {
		t.Errorf("cursor past end = %d", m.cursor)
	}
	m, _ = sbUpdate(t, m, sbKey("up"))
	m, _ = sbUpdate(t, m, sbKey("k"))
	if m.cursor != 0 {
		t.Errorf("cursor = %d", m.cursor)
	}
	m, _ = sbUpdate(t, m, sbKey("k")) // clamp at top
	if m.cursor != 0 {
		t.Errorf("cursor past top = %d", m.cursor)
	}

	// A fresh model's first scan lands the cursor on the displayed item.
	shownSt := dcTmuxState{
		all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
	}
	fresh := sbModel(t, shownSt)
	fresh, _ = sbUpdate(t, fresh, fresh.scanCmd()().(sbScanMsg))
	if !fresh.items[fresh.cursor].Shown {
		t.Errorf("first scan cursor not on shown item (cursor=%d)", fresh.cursor)
	}

	// A rescan that re-orders the list keeps the cursor on the same item.
	m, _ = sbUpdate(t, m, sbKey("j")) // cursor 1, selKey set
	moved := append([]dcCandidate{}, m.items...)
	moved[0], moved[1] = moved[1], moved[0]
	m, _ = sbUpdate(t, m, sbScanMsg{cands: moved})
	if dcKey(m.items[m.cursor]) != m.selKey {
		t.Errorf("cursor lost its item after re-sort: cursor=%d selKey=%q", m.cursor, m.selKey)
	}

	// A rescan whose list no longer contains the tracked item clamps the cursor.
	m.cursor = 2
	m.selKey = "gone\x00gone"
	m, _ = sbUpdate(t, m, sbScanMsg{cands: m.items[:1]})
	if m.cursor != 0 {
		t.Errorf("cursor after shrink = %d", m.cursor)
	}

	// Ticks re-issue their commands; r rescans; q quits.
	if _, cmd := sbUpdate(t, m, sbScanTick{}); cmd == nil {
		t.Error("scan tick returned no cmd")
	}
	if _, cmd := sbUpdate(t, m, sbUsageTick{}); cmd == nil {
		t.Error("usage tick returned no cmd")
	}
	if _, cmd := sbUpdate(t, m, sbKey("r")); cmd == nil {
		t.Error("r returned no cmd")
	}
	if _, cmd := sbUpdate(t, m, sbKey("q")); cmd == nil {
		t.Error("q returned no quit cmd")
	}
	if next, cmd := sbUpdate(t, m, struct{}{}); cmd != nil || next.cursor != m.cursor {
		t.Error("unknown msg mutated model")
	}

	// The tick command delivers its message after the delay.
	if msg := sbAfter(time.Millisecond, sbClearMsg{})(); msg != (sbClearMsg{}) {
		t.Errorf("sbAfter delivered %#v", msg)
	}
}

func TestSidebarUsage(t *testing.T) {
	claudeDir := stubHome(t)
	content := `{"type":"assistant","sessionId":"s1","timestamp":"2026-07-21T10:00:00Z","message":{"usage":{"input_tokens":100,"output_tokens":10}}}` + "\n"
	if err := os.MkdirAll(filepath.Join(claudeDir, "projects", "-w"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "projects", "-w", "a.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m := sbModel(t, dcTmuxState{})
	msg := m.usageCmd()()
	um, ok := msg.(sbUsageMsg)
	if !ok || um.sum.Week.Msgs != 1 {
		t.Fatalf("usage msg = %+v", msg)
	}
	m, cmd := sbUpdate(t, m, um)
	if cmd == nil || m.sum.Week.InputTokens != 100 {
		t.Errorf("sum = %+v", m.sum)
	}

	// Home lookup failure falls back to an empty summary.
	saved := userHomeDir
	userHomeDir = func() (string, error) { return "", fmt.Errorf("no home") }
	t.Cleanup(func() { userHomeDir = saved })
	if msg := m.usageCmd()(); msg.(sbUsageMsg).sum.Week.Msgs != 0 {
		t.Errorf("expected empty summary, got %+v", msg)
	}
}

func TestSidebarAttach(t *testing.T) {
	st := dcTmuxState{
		all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
	}
	m := sbModel(t, st)
	m, _ = sbUpdate(t, m, m.scanCmd()().(sbScanMsg))

	// Enter on a running, already-shown item: focus + status.
	for i, c := range m.items {
		if c.Name == "oasys-ui" {
			m.cursor = i
		}
	}
	m, cmd := sbUpdate(t, m, sbKey("enter"))
	if !strings.Contains(m.status, "oasys-ui") || cmd == nil {
		t.Errorf("status = %q", m.status)
	}
	m, _ = sbUpdate(t, m, sbClearMsg{})
	if m.status != "" {
		t.Errorf("status not cleared: %q", m.status)
	}

	// Enter on a stopped item: brings it up asynchronously, then attaches.
	for i, c := range m.items {
		if c.Name == "gantry" {
			m.cursor = i
		}
	}
	var upCmd tea.Cmd
	m, upCmd = sbUpdate(t, m, sbKey("enter"))
	if !strings.Contains(m.status, "starting") || upCmd == nil {
		t.Fatalf("stopped status = %q", m.status)
	}
	done, ok := upCmd().(sbUpDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("up msg = %#v", done)
	}
	m, _ = sbUpdate(t, m, done)
	if !strings.Contains(m.status, "gantry") {
		t.Errorf("post-up status = %q", m.status)
	}

	// A failed up surfaces the error.
	m, _ = sbUpdate(t, m, sbUpDoneMsg{cand: m.items[m.cursor], err: fmt.Errorf("boom")})
	if !strings.Contains(m.status, "start failed") {
		t.Errorf("failed-up status = %q", m.status)
	}

	// Enter with empty list is a no-op.
	empty := sbModel(t, dcTmuxState{})
	if _, cmd := sbUpdate(t, empty, sbKey("enter")); cmd != nil {
		t.Error("enter on empty list returned a cmd")
	}
}

func TestSidebarAttachError(t *testing.T) {
	st := dcTmuxState{
		all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
	}
	inner := dcHandler(dcFixture(), st)
	fake := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), "swap-pane") {
			return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
		}
		return inner(c)
	}}
	m := newSidebarModel(context.Background(), fake)
	m, _ = sbUpdate(t, m, m.scanCmd()().(sbScanMsg))
	for i, c := range m.items {
		if c.Name == "oasys" {
			m.cursor = i
		}
	}
	m, _ = sbUpdate(t, m, sbKey("enter"))
	if m.status == "" || strings.Contains(m.status, "→") {
		t.Errorf("expected error status, got %q", m.status)
	}
}

func TestSidebarScanErrorKeepsList(t *testing.T) {
	m := sbModel(t, dcTmuxState{})
	m, _ = sbUpdate(t, m, m.scanCmd()().(sbScanMsg))
	before := len(m.items)
	if before == 0 {
		t.Fatal("fixture produced no items")
	}
	m, cmd := sbUpdate(t, m, sbScanMsg{err: fmt.Errorf("docker down")})
	if len(m.items) != before || m.status != "docker unreachable" || cmd == nil {
		t.Errorf("items=%d status=%q", len(m.items), m.status)
	}
	// A successful scan clears the transient status.
	m, _ = sbUpdate(t, m, sbScanMsg{cands: m.items})
	if m.status != "" {
		t.Errorf("status not cleared: %q", m.status)
	}
}

func TestSidebarScrollWindow(t *testing.T) {
	m := sbModel(t, dcTmuxState{})
	for i := 0; i < 12; i++ {
		m.items = append(m.items, dcCandidate{Name: fmt.Sprintf("c%02d", i), Label: fmt.Sprintf("c%02d", i), Folder: fmt.Sprintf("/f/%d", i)})
	}

	// Without a size the whole list renders.
	if s, e := m.listWindow(); s != 0 || e != 12 {
		t.Errorf("no-size window = %d,%d", s, e)
	}

	m, _ = sbUpdate(t, m, tea.WindowSizeMsg{Width: 30, Height: 18}) // avail = 5
	m.cursor = 0
	v := m.View().Content
	if !strings.Contains(v, "↓ 7 more") || strings.Contains(v, "↑ 7 more") {
		t.Errorf("top window markers wrong:\n%s", v)
	}
	m.cursor = 11
	if s, e := m.listWindow(); s != 7 || e != 12 {
		t.Errorf("bottom window = %d,%d", s, e)
	}
	if v := m.View().Content; !strings.Contains(v, "↑ 7 more") {
		t.Errorf("bottom marker missing:\n%s", v)
	}
	m.cursor = 6
	if v := m.View().Content; !strings.Contains(v, "↑ 4 more") || !strings.Contains(v, "↓ 3 more") {
		t.Errorf("middle markers missing:\n%s", v)
	}

	// Tiny heights clamp to a minimum window of 3.
	m, _ = sbUpdate(t, m, tea.WindowSizeMsg{Width: 30, Height: 5})
	if s, e := m.listWindow(); e-s != 3 {
		t.Errorf("tiny window = %d,%d", s, e)
	}

	// Name width follows the pane width.
	if w := m.nameWidth(); w != 22 {
		t.Errorf("nameWidth(30) = %d", w)
	}
	m.width = 0
	if w := m.nameWidth(); w != 20 {
		t.Errorf("nameWidth(0) = %d", w)
	}
	m.width = 100
	if w := m.nameWidth(); w != 40 {
		t.Errorf("nameWidth(100) = %d", w)
	}
}

func TestSidebarClose(t *testing.T) {
	st := dcTmuxState{
		all:  dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
		main: dcPaneLine("%1", "@1", "0", uiFolder, uiConfig) + dcSidebarLine("%2"),
	}

	pick := func(m sidebarModel, name string) sidebarModel {
		for i, c := range m.items {
			if c.Name == name {
				m.cursor = i
			}
		}
		return m
	}

	t.Run("kills the pane", func(t *testing.T) {
		m := sbModel(t, st)
		m, _ = sbUpdate(t, m, m.scanCmd()().(sbScanMsg))
		m = pick(m, "oasys-ui")
		m, cmd := sbUpdate(t, m, sbKey("x"))
		if !strings.Contains(m.status, "✕ oasys-ui") || cmd == nil {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("no pane to close", func(t *testing.T) {
		m := sbModel(t, st)
		m, _ = sbUpdate(t, m, m.scanCmd()().(sbScanMsg))
		m = pick(m, "gantry")
		m, _ = sbUpdate(t, m, sbKey("x"))
		if !strings.Contains(m.status, "no pane") {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("pane vanished between scan and close", func(t *testing.T) {
		n := 0
		inner := dcHandler(dcFixture(), st)
		fake := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "list-panes -s") {
				n++
				if n >= 2 { // the close-time re-lookup finds nothing
					return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
				}
			}
			return inner(c)
		}}
		m := newSidebarModel(context.Background(), fake)
		m, _ = sbUpdate(t, m, m.scanCmd()().(sbScanMsg))
		m = pick(m, "oasys-ui")
		m, _ = sbUpdate(t, m, sbKey("x"))
		if !strings.Contains(m.status, "already gone") {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("kill failure", func(t *testing.T) {
		inner := dcHandler(dcFixture(), st)
		fake := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			if strings.Contains(c.Line(), "kill-pane") {
				return sysexec.Result{ExitCode: 1}, cmdErr(c, 1)
			}
			return inner(c)
		}}
		m := newSidebarModel(context.Background(), fake)
		m, _ = sbUpdate(t, m, m.scanCmd()().(sbScanMsg))
		m = pick(m, "oasys-ui")
		m, _ = sbUpdate(t, m, sbKey("x"))
		if !strings.Contains(m.status, "could not close") {
			t.Errorf("status = %q", m.status)
		}
	})

	t.Run("empty list is a no-op", func(t *testing.T) {
		m := sbModel(t, dcTmuxState{})
		if _, cmd := sbUpdate(t, m, sbKey("x")); cmd != nil {
			t.Error("x on empty list returned a cmd")
		}
	})
}

func TestSidebarView(t *testing.T) {
	m := sbModel(t, dcTmuxState{})
	if v := m.View().Content; !strings.Contains(v, "scanning") {
		t.Errorf("empty view missing scanning hint:\n%s", v)
	}

	m.items = []dcCandidate{
		{Name: "shown", Label: "shown", State: "running", HasWindow: true, Shown: true},
		{Name: "parked", Label: "parked", State: "running", HasWindow: true, PaneTitle: "✳ thinking"},
		{Name: "deadpane", Label: "deadpane", State: "running", HasWindow: true, PaneDead: true},
		{Name: "plain", Label: "plain (alt)", State: "exited"},
	}
	m.status = "hello status"
	v := m.View().Content
	for _, want := range []string{"▶", "*", "✕", "✳", "shown", "plain (alt)", "usage", "today", "7d", "hello status", "attach", "/usage"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestSidebarCommand(t *testing.T) {
	// No home stub here: the program's async usage scan may outlive Run and a
	// cleanup that restores the seam would race it. Reading the real (or
	// missing) ~/.claude is harmless.
	pr, pw := io.Pipe()
	var buf bytes.Buffer
	saved := newSidebarProgram
	newSidebarProgram = func(m tea.Model, opts ...tea.ProgramOption) *tea.Program {
		opts = append(opts, tea.WithInput(pr), tea.WithOutput(&buf), tea.WithWindowSize(40, 30), tea.WithoutSignalHandler())
		return tea.NewProgram(m, opts...)
	}
	t.Cleanup(func() { newSidebarProgram = saved })
	go func() { _, _ = pw.Write([]byte("q")) }()
	defer pw.Close()

	if _, _, err := runCLI(t, dcHandler(dcFixture(), dcTmuxState{}), "", "dc", "sidebar"); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("sidebar rendered nothing")
	}
}
