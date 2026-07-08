package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/engine"
)

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
	}
}

func testModel(t *testing.T) Model {
	t.Helper()
	e := &engine.Engine{Project: domain.Project{Name: "app", Slug: "app", TmuxSession: "weft/app"}}
	m := newModel(context.Background(), e)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return nm.(Model)
}

func send(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func TestSessionsPopulateTable(t *testing.T) {
	m := testModel(t)
	m = send(m, sessionsMsg{sessions: []domain.Session{
		{Name: "alpha", Branch: "weft/alpha", Status: domain.StatusReady},
		{Name: "beta", Branch: "weft/beta", Status: domain.StatusStopped},
	}})
	if len(m.sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(m.sessions))
	}
	if len(m.table.Rows()) != 2 {
		t.Errorf("want 2 table rows, got %d", len(m.table.Rows()))
	}
}

func TestNewFormFlow(t *testing.T) {
	m := testModel(t)
	m = send(m, keyPress("n"))
	if m.mode != modeNew {
		t.Fatalf("n should enter modeNew, got %v", m.mode)
	}
	m = send(m, keyPress("esc"))
	if m.mode != modeList {
		t.Fatalf("esc should return to modeList, got %v", m.mode)
	}
}

func TestDeleteConfirmFlow(t *testing.T) {
	m := testModel(t)
	m = send(m, sessionsMsg{sessions: []domain.Session{{Name: "alpha", Status: domain.StatusReady}}})
	m = send(m, keyPress("d"))
	if m.mode != modeConfirmDelete || m.deleteTarget != "alpha" {
		t.Fatalf("d should confirm delete of alpha, got mode=%v target=%q", m.mode, m.deleteTarget)
	}
	// Any non-y cancels.
	m = send(m, keyPress("n"))
	if m.mode != modeList {
		t.Fatalf("non-y should cancel, got %v", m.mode)
	}
}

func TestCreatingLogsAndDone(t *testing.T) {
	m := testModel(t)
	m.mode = modeCreating
	m.createName = "gamma"
	m = send(m, createEventMsg{ev: engine.Event{Kind: engine.EventStep, Step: "worktree"}})
	m = send(m, createEventMsg{ev: engine.Event{Kind: engine.EventLog, Text: "pulling image"}})
	if len(m.createLogs) != 2 {
		t.Fatalf("want 2 log lines, got %d", len(m.createLogs))
	}
	m = send(m, createDoneMsg{})
	if m.mode != modeList {
		t.Errorf("createDone should return to modeList")
	}
	if !strings.Contains(m.toast, "created gamma") {
		t.Errorf("toast = %q", m.toast)
	}
}

func TestViewRendersEveryMode(t *testing.T) {
	m := testModel(t)
	m = send(m, sessionsMsg{sessions: []domain.Session{{Name: "alpha", Branch: "weft/alpha", Status: domain.StatusReady}}})

	// List mode: the frame must contain the title, column headers, and the row.
	list := m.View().Content
	for _, want := range []string{"weft", "project:", "STATUS", "NAME", "BRANCH", "CONTAINER", "CLAUDE", "alpha"} {
		if !strings.Contains(list, want) {
			t.Errorf("list view missing %q", want)
		}
	}
	if !m.View().AltScreen {
		t.Error("view should request the alt screen")
	}

	for _, md := range []mode{modeList, modeNew, modeConfirmDelete, modeCreating} {
		m.mode = md
		if v := m.View(); v.Content == "" {
			t.Errorf("mode %v rendered empty view", md)
		}
	}
}
