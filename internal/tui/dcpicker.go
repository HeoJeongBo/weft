package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// DcItem is one discovered devcontainer shown in the picker.
type DcItem struct {
	Name      string
	Container string
	Workspace string
	State     string // running, exited, ...
	HasWindow bool   // a weft/dc tmux window for it already exists
}

// PickDc outcomes besides a selected index.
const (
	DcCancelled = -1 // user backed out
	DcRescan    = -2 // user asked for a fresh scan
)

// PickDc shows a wifi-scan-style picker over items and returns the selected
// index, DcCancelled, or DcRescan (the caller re-scans and calls again).
func PickDc(ctx context.Context, items []DcItem) (int, error) {
	m := dcModel{theme: newTheme(), items: items, result: DcCancelled}
	out, err := newProgram(m, tea.WithContext(ctx)).Run()
	if err != nil {
		return DcCancelled, err
	}
	return out.(dcModel).result, nil
}

// dcModel is the picker's Bubble Tea model.
type dcModel struct {
	theme  theme
	items  []DcItem
	cursor int
	result int
}

func (m dcModel) Init() tea.Cmd { return nil }

func (m dcModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.items) > 0 {
			m.result = m.cursor
			return m, tea.Quit
		}
	case "r":
		m.result = DcRescan
		return m, tea.Quit
	case "q", "esc", "ctrl+c":
		m.result = DcCancelled
		return m, tea.Quit
	}
	return m, nil
}

func (m dcModel) View() tea.View {
	var b strings.Builder
	b.WriteString(m.theme.title.Render("devcontainers") + "\n\n")
	if len(m.items) == 0 {
		b.WriteString(m.theme.footer.Render("none found — open one in your editor or run `devcontainer up`") + "\n")
	}
	for i, it := range m.items {
		glyph, col := "○", m.theme.muted
		if it.State == "running" {
			glyph, col = "●", m.theme.success
		}
		name := it.Name
		if it.HasWindow {
			name += "*"
		}
		line := fmt.Sprintf("%s %-8s %-18s %-28s %s",
			glyph, it.State, dcTrunc(name, 18), dcTrunc(it.Container, 28), it.Workspace)
		style := lipgloss.NewStyle().Foreground(col)
		prefix := "  "
		if i == m.cursor {
			style = lipgloss.NewStyle().Foreground(m.theme.accent).Bold(true)
			prefix = "❯ "
		}
		b.WriteString(style.Render(prefix+line) + "\n")
	}
	b.WriteString("\n" + m.theme.footer.Render("↑/↓ move · enter attach claude · r rescan · q quit · * window open") + "\n")
	return tea.NewView(b.String())
}

func dcTrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
