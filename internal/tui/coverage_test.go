package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/HeoJeongBo/weft/internal/config"
	"github.com/HeoJeongBo/weft/internal/devcontainer"
	"github.com/HeoJeongBo/weft/internal/dockerx"
	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/engine"
	"github.com/HeoJeongBo/weft/internal/git"
	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/tmux"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// --- test doubles / helpers ---------------------------------------------------

const (
	tuiWorktrees = `worktree /repo
HEAD aaaa
branch refs/heads/main

worktree /home/u/.weft/worktrees/app/feat-auth
HEAD bbbb
branch refs/heads/weft/feat-auth
`
	tuiDockerRunning = `{"ID":"c1","Image":"go:1.24","State":"running","Labels":"weft.project=app,weft.session=app/feat-auth"}`
)

// tuiEngine builds an Engine over a FakeRunner. Devcontainer is disabled so the
// lifecycle commands stay simple; callers tweak Cfg as needed.
func tuiEngine(t *testing.T, handler func(sysexec.Call) (sysexec.Result, error)) (*engine.Engine, *sysexec.FakeRunner) {
	t.Helper()
	f := &sysexec.FakeRunner{Handler: handler}
	cfg := config.Defaults()
	cfg.Devcontainer.Enabled = false
	e := &engine.Engine{
		Runner:  f,
		Git:     git.New(f, "/repo"),
		Tmux:    tmux.New(f),
		Docker:  dockerx.New(f),
		DC:      devcontainer.New(f),
		Cfg:     cfg,
		Project: domain.Project{Name: "app", Slug: "app", Root: "/repo", DefaultBranch: "main", TmuxSession: "weft/app"},
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return e, f
}

// okReconcile answers a reconcile that yields the feat-auth session (worktree +
// running container, no window).
func okReconcile(c sysexec.Call) (sysexec.Result, error) {
	line := c.Line()
	switch {
	case strings.Contains(line, "worktree list"):
		return sysexec.Result{Stdout: tuiWorktrees}, nil
	case strings.Contains(line, "docker ps"):
		return sysexec.Result{Stdout: tuiDockerRunning}, nil
	default:
		return sysexec.Result{}, nil
	}
}

// modelOver builds a sized Model over the given engine.
func modelOver(t *testing.T, e *engine.Engine) Model {
	t.Helper()
	m := newModel(context.Background(), e)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return nm.(Model)
}

// --- commands.go --------------------------------------------------------------

func TestReconcileCmd(t *testing.T) {
	e, _ := tuiEngine(t, okReconcile)
	msg := reconcileCmd(context.Background(), e)()
	sm, ok := msg.(sessionsMsg)
	if !ok {
		t.Fatalf("want sessionsMsg, got %T", msg)
	}
	if len(sm.sessions) == 0 {
		t.Fatal("want at least one session")
	}

	ee, _ := tuiEngine(t, func(c sysexec.Call) (sysexec.Result, error) {
		if strings.Contains(c.Line(), "worktree list") {
			return sysexec.Result{ExitCode: 1}, &wefterr.CmdError{Cmd: "git", Args: c.Args, ExitCode: 1}
		}
		return sysexec.Result{}, nil
	})
	if msg := reconcileCmd(context.Background(), ee)(); func() bool { _, ok := msg.(sessionsErrMsg); return !ok }() {
		t.Fatalf("want sessionsErrMsg, got %T", msg)
	}
}

func TestStartCreateCmd(t *testing.T) {
	// feat-auth already exists → New emits one EventError then closes the channel.
	e, _ := tuiEngine(t, okReconcile)
	msg := startCreateCmd(context.Background(), e, engine.NewSpec{Name: "feat-auth"})()
	cs, ok := msg.(createStartedMsg)
	if !ok {
		t.Fatalf("want createStartedMsg, got %T", msg)
	}
	var events int
	for range cs.ch {
		events++
	}
	if events == 0 {
		t.Fatal("expected at least one event before the channel closed")
	}
}

func TestWaitForEvent(t *testing.T) {
	// step → createEventMsg
	step := make(chan engine.Event, 1)
	step <- engine.Event{Kind: engine.EventStep, Step: "worktree"}
	if _, ok := waitForEvent(step)().(createEventMsg); !ok {
		t.Fatal("step should map to createEventMsg")
	}

	// error → createDoneMsg{err}
	fail := make(chan engine.Event, 1)
	boom := errors.New("boom")
	fail <- engine.Event{Kind: engine.EventError, Err: boom}
	if m := waitForEvent(fail)().(createDoneMsg); m.err != boom {
		t.Fatalf("error event err = %v", m.err)
	}

	// closed → createDoneMsg{}
	done := make(chan engine.Event)
	close(done)
	if m := waitForEvent(done)().(createDoneMsg); m.err != nil {
		t.Fatalf("closed channel err = %v", m.err)
	}
}

func TestActionCmds(t *testing.T) {
	tests := []struct {
		name    string
		handler func(sysexec.Call) (sysexec.Result, error)
		cmd     func(*engine.Engine) tea.Cmd
		wantErr bool
	}{
		{"stop-ok", okReconcile, func(e *engine.Engine) tea.Cmd { return stopCmd(context.Background(), e, "feat-auth") }, false},
		{"stop-missing", nil, func(e *engine.Engine) tea.Cmd { return stopCmd(context.Background(), e, "feat-auth") }, true},
		{"start-ok", okReconcile, func(e *engine.Engine) tea.Cmd { return startCmd(context.Background(), e, "feat-auth") }, false},
		{"start-missing", nil, func(e *engine.Engine) tea.Cmd { return startCmd(context.Background(), e, "feat-auth") }, true},
		{"remove-ok", okReconcile, func(e *engine.Engine) tea.Cmd { return removeCmd(context.Background(), e, "feat-auth") }, false},
		{"remove-missing", nil, func(e *engine.Engine) tea.Cmd { return removeCmd(context.Background(), e, "feat-auth") }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := tuiEngine(t, tc.handler)
			e.Cfg.Cleanup.RequireClean = false // avoid the dirty guard for remove-ok
			msg := tc.cmd(e)()
			ad, ok := msg.(actionDoneMsg)
			if !ok {
				t.Fatalf("want actionDoneMsg, got %T", msg)
			}
			if tc.wantErr && ad.err == nil {
				t.Fatalf("%s: want error, got none (note=%q)", tc.name, ad.note)
			}
			if !tc.wantErr && ad.err != nil {
				t.Fatalf("%s: unexpected error %v", tc.name, ad.err)
			}
		})
	}
}

func TestToTickMsg(t *testing.T) {
	if _, ok := toTickMsg(time.Now()).(tickMsg); !ok {
		t.Fatal("toTickMsg should produce tickMsg")
	}
}

func TestAttachCmdInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "x")
	e, f := tuiEngine(t, nil)
	cmd := attachCmd(context.Background(), e, "feat-auth")
	if cmd == nil {
		t.Fatal("attachCmd returned nil")
	}
	msg := cmd()
	ad, ok := msg.(attachDoneMsg)
	if !ok {
		t.Fatalf("want attachDoneMsg, got %T", msg)
	}
	if ad.err != nil {
		t.Fatalf("switch-client should succeed: %v", ad.err)
	}
	var sawSelect, sawSwitch bool
	for _, c := range f.Calls {
		line := c.Line()
		sawSelect = sawSelect || strings.Contains(line, "select-window")
		sawSwitch = sawSwitch || strings.Contains(line, "switch-client")
	}
	if !sawSelect || !sawSwitch {
		t.Errorf("expected select-window and switch-client (select=%v switch=%v)", sawSelect, sawSwitch)
	}
}

func TestAttachCmdOutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	e, _ := tuiEngine(t, nil)
	// Constructs the ExecProcess command; we must NOT run the returned Cmd.
	if cmd := attachCmd(context.Background(), e, "feat-auth"); cmd == nil {
		t.Fatal("attachCmd should return a non-nil exec command")
	}
}

func TestAttachResult(t *testing.T) {
	boom := errors.New("x")
	if m := attachResult(boom).(attachDoneMsg); m.err != boom {
		t.Fatalf("attachResult(err) = %v", m.err)
	}
	if m := attachResult(nil).(attachDoneMsg); m.err != nil {
		t.Fatalf("attachResult(nil) = %v", m.err)
	}
}

// --- model.go Update ----------------------------------------------------------

func TestUpdateMessages(t *testing.T) {
	// sessionsErrMsg
	m := testModel(t)
	m = send(m, sessionsErrMsg{err: errors.New("docker down")})
	if !strings.Contains(m.toast, "reconcile: docker down") {
		t.Errorf("sessionsErrMsg toast = %q", m.toast)
	}

	// actionDoneMsg: error path
	m = send(m, actionDoneMsg{note: "ignored", err: errors.New("kaboom")})
	if m.toast != "kaboom" {
		t.Errorf("actionDoneMsg err toast = %q", m.toast)
	}
	// actionDoneMsg: note path
	m = send(m, actionDoneMsg{note: "all good"})
	if m.toast != "all good" {
		t.Errorf("actionDoneMsg note toast = %q", m.toast)
	}

	// attachDoneMsg: error + nil
	m = send(m, attachDoneMsg{err: errors.New("no client")})
	if !strings.Contains(m.toast, "attach: no client") {
		t.Errorf("attachDoneMsg err toast = %q", m.toast)
	}
	m.toast = "keep"
	m = send(m, attachDoneMsg{})
	if m.toast != "keep" {
		t.Errorf("nil attachDoneMsg should not clobber toast, got %q", m.toast)
	}

	// tickMsg in modeList vs not.
	m.mode = modeList
	if _, cmd := m.Update(tickMsg(time.Now())); cmd == nil {
		t.Error("tickMsg in modeList should batch reconcile + tick")
	}
	m.mode = modeNew
	if _, cmd := m.Update(tickMsg(time.Now())); cmd == nil {
		t.Error("tickMsg should still schedule the next tick")
	}
	m.mode = modeList

	// createStartedMsg
	ch := make(chan engine.Event)
	m2, cmd := m.Update(createStartedMsg{ch: ch})
	if cmd == nil {
		t.Error("createStartedMsg should return a batch cmd")
	}
	if m2.(Model).createCh == nil {
		t.Error("createStartedMsg should store the channel")
	}

	// createEventMsg: step, log, other.
	m = testModel(t)
	m.mode = modeCreating
	m.createCh = ch
	m = send(m, createEventMsg{ev: engine.Event{Kind: engine.EventStep, Step: "worktree"}})
	m = send(m, createEventMsg{ev: engine.Event{Kind: engine.EventLog, Text: "pulling"}})
	m = send(m, createEventMsg{ev: engine.Event{Kind: engine.EventDone}}) // other → no log
	if len(m.createLogs) != 2 {
		t.Errorf("want 2 logs after step+log+done, got %d", len(m.createLogs))
	}

	// createDoneMsg error.
	m.createName = "gamma"
	m = send(m, createDoneMsg{err: errors.New("build failed")})
	if m.mode != modeList || !strings.Contains(m.toast, "create failed: build failed") {
		t.Errorf("createDoneMsg err: mode=%v toast=%q", m.mode, m.toast)
	}

	// spinner.TickMsg in modeCreating and not.
	m.mode = modeCreating
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil {
		_ = cmd // in modeCreating the spinner command is forwarded
	}
	m.mode = modeList
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil {
		t.Error("spinner tick outside modeCreating should not forward a cmd")
	}
}

// --- model.go updateList ------------------------------------------------------

func TestUpdateListKeys(t *testing.T) {
	t.Setenv("TMUX", "")
	e, _ := tuiEngine(t, okReconcile)

	withSession := func() Model {
		m := modelOver(t, e)
		return send(m, sessionsMsg{sessions: []domain.Session{{Name: "feat-auth", Branch: "weft/feat-auth", Status: domain.StatusReady}}})
	}
	empty := func() Model { return modelOver(t, e) }

	// Quit.
	if _, cmd := withSession().Update(keyPress("q")); cmd == nil {
		t.Error("q should return tea.Quit")
	}
	// Help toggles.
	m := withSession()
	m = send(m, keyPress("?"))
	if !m.showHelp {
		t.Error("? should toggle help on")
	}
	m = send(m, keyPress("?"))
	if m.showHelp {
		t.Error("? should toggle help off")
	}
	// Refresh clears toast.
	m = withSession()
	m.toast = "stale"
	m = send(m, keyPress("r"))
	if m.toast != "" {
		t.Errorf("r should clear toast, got %q", m.toast)
	}
	// New.
	if send(withSession(), keyPress("n")).mode != modeNew {
		t.Error("n should enter modeNew")
	}
	// Attach with selection, and none.
	if _, cmd := withSession().Update(keyPress("enter")); cmd == nil {
		t.Error("enter with selection should return attach cmd")
	}
	if _, cmd := empty().Update(keyPress("enter")); cmd != nil {
		t.Error("enter with no selection should be a no-op")
	}
	// Stop with selection, and none.
	if m := send(withSession(), keyPress("s")); !strings.Contains(m.toast, "stopping feat-auth") {
		t.Errorf("s toast = %q", m.toast)
	}
	if _, cmd := empty().Update(keyPress("s")); cmd != nil {
		t.Error("s with no selection should be a no-op")
	}
	// Start with selection, and none.
	if m := send(withSession(), keyPress("S")); !strings.Contains(m.toast, "starting feat-auth") {
		t.Errorf("S toast = %q", m.toast)
	}
	if _, cmd := empty().Update(keyPress("S")); cmd != nil {
		t.Error("S with no selection should be a no-op")
	}
	// Delete with selection, and none.
	if m := send(withSession(), keyPress("d")); m.mode != modeConfirmDelete || m.deleteTarget != "feat-auth" {
		t.Errorf("d: mode=%v target=%q", m.mode, m.deleteTarget)
	}
	if send(empty(), keyPress("d")).mode != modeList {
		t.Error("d with no selection should stay in list")
	}
	// default → table (movement keys).
	send(withSession(), keyPress("j"))
	send(withSession(), keyPress("g"))
}

// --- model.go updateNew -------------------------------------------------------

func TestUpdateNew(t *testing.T) {
	e, _ := tuiEngine(t, okReconcile)

	// Cancel.
	m := modelOver(t, e)
	m.mode = modeNew
	if send(m, keyPress("esc")).mode != modeList {
		t.Error("esc should cancel modeNew")
	}

	// enter valid → modeCreating.
	m = modelOver(t, e)
	m.mode = modeNew
	m.input.SetValue("feat")
	m2, cmd := m.Update(keyPress("enter"))
	nm := m2.(Model)
	if nm.mode != modeCreating || nm.createName != "feat" || cmd == nil {
		t.Errorf("valid enter: mode=%v name=%q cmd=%v", nm.mode, nm.createName, cmd != nil)
	}
	if nm.cancelCreate == nil {
		t.Error("valid enter should set cancelCreate")
	}

	// enter invalid → toast, stay in modeNew.
	m = modelOver(t, e)
	m.mode = modeNew
	m.input.SetValue("bad name")
	m = send(m, keyPress("enter"))
	if m.mode != modeNew || !strings.Contains(m.toast, "invalid name") {
		t.Errorf("invalid enter: mode=%v toast=%q", m.mode, m.toast)
	}

	// typing → input.Update.
	m = modelOver(t, e)
	m.mode = modeNew
	if _, cmd := m.Update(keyPress("a")); cmd == nil {
		// textinput may or may not return a cmd; just ensure the branch runs.
		_ = cmd
	}
}

// --- model.go updateConfirm ---------------------------------------------------

func TestUpdateConfirm(t *testing.T) {
	e, _ := tuiEngine(t, okReconcile)
	base := func() Model {
		m := modelOver(t, e)
		m.mode = modeConfirmDelete
		m.deleteTarget = "feat-auth"
		return m
	}
	// y
	m := send(base(), keyPress("y"))
	if m.mode != modeList || !strings.Contains(m.toast, "removing feat-auth") {
		t.Errorf("y: mode=%v toast=%q", m.mode, m.toast)
	}
	// Y
	m = send(base(), keyPress("Y"))
	if !strings.Contains(m.toast, "removing feat-auth") {
		t.Errorf("Y toast = %q", m.toast)
	}
	// other
	m = send(base(), keyPress("n"))
	if m.mode != modeList {
		t.Errorf("other key should cancel, mode=%v", m.mode)
	}
}

// --- model.go updateCreating --------------------------------------------------

func TestUpdateCreating(t *testing.T) {
	e, _ := tuiEngine(t, okReconcile)

	// cancel with cancelCreate set.
	m := modelOver(t, e)
	m.mode = modeCreating
	called := false
	m.cancelCreate = func() { called = true }
	m = send(m, keyPress("esc"))
	if !called || !strings.Contains(m.toast, "cancelling") {
		t.Errorf("esc should cancel: called=%v toast=%q", called, m.toast)
	}

	// cancel with nil cancelCreate → no-op.
	m = modelOver(t, e)
	m.mode = modeCreating
	m.cancelCreate = nil
	m = send(m, keyPress("esc"))
	if m.toast != "" {
		t.Errorf("esc with nil cancel should be a no-op, toast=%q", m.toast)
	}

	// non-cancel key in creating → no-op branch.
	m = modelOver(t, e)
	m.mode = modeCreating
	m = send(m, keyPress("x"))
	if m.toast != "" {
		t.Errorf("other key in creating should be a no-op, toast=%q", m.toast)
	}
}

// --- model.go setSize ---------------------------------------------------------

func TestSetSize(t *testing.T) {
	e, _ := tuiEngine(t, okReconcile)

	// width 0 → early return (no columns set).
	m := newModel(context.Background(), e)
	m = send(m, tea.WindowSizeMsg{Width: 0, Height: 0})
	if len(m.table.Columns()) != 0 {
		t.Error("width 0 should short-circuit before setting columns")
	}

	// tiny width → rest clamps to 24.
	m = send(m, tea.WindowSizeMsg{Width: 20, Height: 30})
	if len(m.table.Columns()) != 5 {
		t.Errorf("want 5 columns, got %d", len(m.table.Columns()))
	}

	// tiny height → h clamps to 3.
	send(m, tea.WindowSizeMsg{Width: 100, Height: 2})
}

// --- model.go selected & appendLog -------------------------------------------

func TestSelectedAndAppendLog(t *testing.T) {
	m := testModel(t)
	// out of range (no sessions).
	if _, ok := m.selected(); ok {
		t.Error("selected should be false with no sessions")
	}
	// in range.
	m = send(m, sessionsMsg{sessions: []domain.Session{{Name: "a"}}})
	if s, ok := m.selected(); !ok || s.Name != "a" {
		t.Errorf("selected = %+v, %v", s, ok)
	}

	// appendLog trims to 200.
	logs := make([]string, 200)
	logs = appendLog(logs, "overflow")
	if len(logs) != 200 {
		t.Errorf("appendLog should cap at 200, got %d", len(logs))
	}
	if logs[199] != "overflow" {
		t.Errorf("newest line should be retained, got %q", logs[199])
	}
	// under the cap.
	if got := appendLog([]string{"a"}, "b"); len(got) != 2 {
		t.Errorf("appendLog under cap = %v", got)
	}
}

// --- view.go ------------------------------------------------------------------

func TestViewsCoverBranches(t *testing.T) {
	m := testModel(t)

	// creatingView with > 12 log lines.
	m.mode = modeCreating
	m.createName = "gamma"
	for i := 0; i < 15; i++ {
		m.createLogs = append(m.createLogs, "line")
	}
	if v := m.creatingView(); v == "" {
		t.Error("creatingView should render")
	}

	// footerView with showHelp.
	m.mode = modeList
	m.showHelp = true
	if !strings.Contains(m.footerView(), "up") {
		t.Error("full help should include the up binding")
	}

	// footerView with modeConfirmDelete + toast.
	m.showHelp = false
	m.mode = modeConfirmDelete
	m.deleteTarget = "alpha"
	m.toast = "hi"
	f := m.footerView()
	if !strings.Contains(f, "delete session") || !strings.Contains(f, "hi") {
		t.Errorf("footer = %q", f)
	}
}

func TestContainerCell(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"running", "up"},
		{"exited", "down"},
		{"dead", "down"},
		{"created", "created"},
	}
	// nil container.
	if got := containerCell(domain.Session{}); got != "—" {
		t.Errorf("nil container = %q", got)
	}
	for _, tc := range tests {
		s := domain.Session{Container: &domain.Container{State: tc.state}}
		if got := containerCell(s); got != tc.want {
			t.Errorf("container %q = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestClaudeCell(t *testing.T) {
	if got := claudeCell(domain.ClaudeNone); got != "—" {
		t.Errorf("none = %q", got)
	}
	if got := claudeCell(domain.ClaudeRunning); got != "running" {
		t.Errorf("running = %q", got)
	}
}

// --- theme.go -----------------------------------------------------------------

func TestThemeStatus(t *testing.T) {
	th := newTheme()
	statuses := []domain.SessionStatus{
		domain.StatusReady,
		domain.StatusStarting,
		domain.StatusDetached,
		domain.StatusStopped,
		domain.StatusPartial,
		domain.StatusOrphaned,
		domain.StatusUnknown,
		domain.SessionStatus("bogus"),
	}
	for _, s := range statuses {
		if th.statusColor(s) == nil {
			t.Errorf("statusColor(%s) is nil", s)
		}
		if statusGlyph(s) == "" {
			t.Errorf("statusGlyph(%s) is empty", s)
		}
		if th.statusCell(s) == "" {
			t.Errorf("statusCell(%s) is empty", s)
		}
	}
	// Confirm the specific glyphs so every switch arm is asserted.
	wantGlyph := map[domain.SessionStatus]string{
		domain.StatusReady:    "●",
		domain.StatusStarting: "◐",
		domain.StatusDetached: "◒",
		domain.StatusStopped:  "○",
		domain.StatusPartial:  "◑",
		domain.StatusOrphaned: "✕",
		domain.StatusUnknown:  "?",
	}
	for s, g := range wantGlyph {
		if statusGlyph(s) != g {
			t.Errorf("statusGlyph(%s) = %q, want %q", s, statusGlyph(s), g)
		}
	}
}

// --- Run (live) ---------------------------------------------------------------

func TestRunLive(t *testing.T) {
	// Engine over a fake that returns empty stdout for every reconcile query, so
	// Init's reconcile completes with no sessions and the tick never fires before
	// we quit.
	e, _ := tuiEngine(t, nil)

	pr, pw := io.Pipe()
	var buf bytes.Buffer

	orig := newProgram
	defer func() { newProgram = orig }()
	newProgram = func(m tea.Model, opts ...tea.ProgramOption) *tea.Program {
		opts = append(opts,
			tea.WithInput(pr),
			tea.WithOutput(&buf),
			tea.WithWindowSize(100, 30),
			tea.WithoutSignalHandler(),
		)
		return orig(m, opts...)
	}

	// Feed a quit keystroke; keep the writer open until the test ends.
	go func() { _, _ = pw.Write([]byte("q")) }()
	defer pw.Close()

	if err := Run(context.Background(), e); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected rendered output")
	}
}
