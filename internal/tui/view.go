package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/HeoJeongBo/weft/internal/domain"
)

// View implements tea.Model.
func (m Model) View() tea.View {
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n\n")

	switch m.mode {
	case modeNew:
		b.WriteString(m.newView())
	case modeCreating:
		b.WriteString(m.creatingView())
	default:
		b.WriteString(m.table.View())
	}

	b.WriteString("\n")
	b.WriteString(m.footerView())

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "weft — " + m.engine.Project.Name
	return v
}

func (m Model) headerView() string {
	title := m.theme.title.Render("weft")
	meta := m.theme.header.Render(fmt.Sprintf("  project: %s · %d session(s)", m.engine.Project.Name, len(m.sessions)))
	return title + meta
}

func (m Model) newView() string {
	prompt := m.theme.prompt.Render("new session")
	hint := m.theme.footer.Render("enter to create · esc to cancel")
	return fmt.Sprintf("%s\n\n  %s\n\n%s", prompt, m.input.View(), hint)
}

func (m Model) creatingView() string {
	head := fmt.Sprintf("%s creating %s", m.spinner.View(), m.theme.title.Render(m.createName))
	tail := m.createLogs
	const show = 12
	if len(tail) > show {
		tail = tail[len(tail)-show:]
	}
	body := m.theme.logs.Render(strings.Join(tail, "\n"))
	return head + "\n\n" + body
}

func (m Model) footerView() string {
	var lines []string
	if m.mode == modeConfirmDelete {
		lines = append(lines, m.theme.prompt.Render(fmt.Sprintf("delete session %q? (y/N)", m.deleteTarget)))
	}
	if m.toast != "" {
		lines = append(lines, m.theme.toast.Render(m.toast))
	}
	help := m.help.ShortHelpView(m.keys.ShortHelp())
	if m.showHelp {
		help = m.help.FullHelpView(m.keys.FullHelp())
	}
	lines = append(lines, help)
	return strings.Join(lines, "\n")
}

func containerCell(s domain.Session) string {
	if s.Container == nil {
		return "—"
	}
	switch s.Container.State {
	case "running":
		return "up"
	case "exited", "dead":
		return "down"
	default:
		return s.Container.State
	}
}

func claudeCell(c domain.ClaudeState) string {
	if c == domain.ClaudeNone {
		return "—"
	}
	return string(c)
}
