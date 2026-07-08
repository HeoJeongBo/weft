package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/HeoJeongBo/weft/internal/domain"
)

// ANSI color codes.
const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

func colorize(s, code string, on bool) string {
	if !on {
		return s
	}
	return code + s + ansiReset
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// statusGlyph returns the glyph and color code for a status.
func statusGlyph(s domain.SessionStatus) (glyph, code string) {
	switch s {
	case domain.StatusReady:
		return "●", ansiGreen
	case domain.StatusStarting:
		return "◐", ansiYellow
	case domain.StatusStopped:
		return "○", ansiDim
	case domain.StatusPartial:
		return "◑", ansiYellow
	case domain.StatusOrphaned:
		return "✕", ansiRed
	default:
		return "?", ansiDim
	}
}

// statusBadge returns a colored glyph for a status (visible width 1).
func statusBadge(s domain.SessionStatus, color bool) string {
	glyph, code := statusGlyph(s)
	return colorize(glyph, code, color)
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

// printSessionsTable renders the reconciled session list.
func printSessionsTable(w io.Writer, sessions []domain.Session, color bool) {
	if len(sessions) == 0 {
		fmt.Fprintln(w, "no sessions yet — create one with: weft new <name>")
		return
	}
	// The colored glyph has visible width 1; padded plain columns follow, so the
	// invisible ANSI escapes never disturb alignment.
	fmt.Fprintf(w, "  %-8s %-18s %-26s %-9s %s\n", "STATUS", "NAME", "BRANCH", "CONTAINER", "CLAUDE")
	for _, s := range sessions {
		fmt.Fprintf(w, "%s %-8s %-18s %-26s %-9s %s\n",
			statusBadge(s.Status, color),
			string(s.Status),
			truncate(s.Name, 18),
			truncate(s.Branch, 26),
			containerCell(s),
			claudeCell(s.Claude),
		)
	}
}

// printSessionDetail renders a single session for `weft status`.
func printSessionDetail(w io.Writer, s domain.Session, proj domain.Project, color bool) {
	fmt.Fprintf(w, "%s %s  (%s)\n", statusBadge(s.Status, color), s.Name, s.Status)
	fmt.Fprintf(w, "  branch     %s\n", s.Branch)
	if s.Worktree != nil {
		dirty := ""
		if s.Worktree.Dirty {
			dirty = colorize("  •dirty", ansiYellow, color)
		}
		fmt.Fprintf(w, "  worktree   %s%s\n", s.Worktree.Path, dirty)
		fmt.Fprintf(w, "  commits    +%d ahead / -%d behind %s\n", s.Worktree.Ahead, s.Worktree.Behind, proj.DefaultBranch)
	}
	if s.Container != nil {
		fmt.Fprintf(w, "  container  %s (%s)  %s\n", short(s.Container.ID), s.Container.State, s.Container.Image)
	}
	if s.Window != nil {
		fmt.Fprintf(w, "  tmux       %s:%d\n", proj.TmuxSession, s.Window.Index)
	}
	fmt.Fprintf(w, "  claude     %s\n", claudeCell(s.Claude))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
