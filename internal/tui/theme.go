package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/HeoJeongBo/weft/internal/domain"
)

// theme holds the styles used across the dashboard.
type theme struct {
	accent  color.Color
	subtle  color.Color
	muted   color.Color
	success color.Color
	warn    color.Color
	danger  color.Color

	header lipgloss.Style
	title  lipgloss.Style
	footer lipgloss.Style
	toast  lipgloss.Style
	prompt lipgloss.Style
	logs   lipgloss.Style
}

func newTheme() theme {
	accent := lipgloss.Color("6")   // cyan
	subtle := lipgloss.Color("240") // grey
	muted := lipgloss.Color("245")
	success := lipgloss.Color("2")
	warn := lipgloss.Color("3")
	danger := lipgloss.Color("1")

	return theme{
		accent:  accent,
		subtle:  subtle,
		muted:   muted,
		success: success,
		warn:    warn,
		danger:  danger,
		title:   lipgloss.NewStyle().Bold(true).Foreground(accent),
		header:  lipgloss.NewStyle().Foreground(muted),
		footer:  lipgloss.NewStyle().Foreground(subtle),
		toast:   lipgloss.NewStyle().Foreground(accent),
		prompt:  lipgloss.NewStyle().Foreground(warn),
		logs:    lipgloss.NewStyle().Foreground(subtle),
	}
}

// statusColor returns the accent color for a status.
func (t theme) statusColor(s domain.SessionStatus) color.Color {
	switch s {
	case domain.StatusReady:
		return t.success
	case domain.StatusStarting, domain.StatusPartial:
		return t.warn
	case domain.StatusOrphaned:
		return t.danger
	default:
		return t.muted
	}
}

// statusGlyph returns the glyph for a status.
func statusGlyph(s domain.SessionStatus) string {
	switch s {
	case domain.StatusReady:
		return "●"
	case domain.StatusStarting:
		return "◐"
	case domain.StatusStopped:
		return "○"
	case domain.StatusPartial:
		return "◑"
	case domain.StatusOrphaned:
		return "✕"
	default:
		return "?"
	}
}

// statusCell renders a colored "glyph status" for the table.
func (t theme) statusCell(s domain.SessionStatus) string {
	return lipgloss.NewStyle().Foreground(t.statusColor(s)).Render(statusGlyph(s) + " " + string(s))
}
