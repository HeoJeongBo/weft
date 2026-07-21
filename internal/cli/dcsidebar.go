package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/sysexec"
	"github.com/HeoJeongBo/weft/internal/tmux"
	"github.com/HeoJeongBo/weft/internal/usage"
)

// newSidebarProgram is a seam so tests can run the sidebar without a terminal.
var newSidebarProgram = tea.NewProgram

func newDcSidebarCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sidebar",
		Short: "The grid's sidebar UI (weft dc starts this pane automatically)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, _, _ := dcRunner(cmd)
			m := newSidebarModel(cmd.Context(), r)
			_, err := newSidebarProgram(m, tea.WithContext(cmd.Context())).Run()
			return err
		},
	}
}

type (
	sbScanMsg   struct{ cands []dcCandidate }
	sbUsageMsg  struct{ sum usage.Summary }
	sbScanTick  struct{}
	sbUsageTick struct{}
	sbClearMsg  struct{}
)

// sidebarModel is the Orca-style left rail: the devcontainer list on top and a
// usage summary below, refreshed on ticks.
type sidebarModel struct {
	ctx    context.Context
	r      sysexec.Runner
	items  []dcCandidate
	cursor int
	sum    usage.Summary
	status string
}

func newSidebarModel(ctx context.Context, r sysexec.Runner) sidebarModel {
	return sidebarModel{ctx: ctx, r: r}
}

func (m sidebarModel) Init() tea.Cmd {
	return tea.Batch(m.scanCmd(), m.usageCmd())
}

func (m sidebarModel) scanCmd() tea.Cmd {
	return func() tea.Msg {
		cands, _ := dcScan(m.ctx, m.r)
		return sbScanMsg{cands}
	}
}

func (m sidebarModel) usageCmd() tea.Cmd {
	return func() tea.Msg {
		home, err := userHomeDir()
		if err != nil {
			return sbUsageMsg{}
		}
		return sbUsageMsg{usage.Summarize(filepath.Join(home, ".claude"), time.Now())}
	}
}

func sbAfter(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

func (m sidebarModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case sbScanMsg:
		m.items = msg.cands
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
		}
		return m, sbAfter(3*time.Second, sbScanTick{})
	case sbUsageMsg:
		m.sum = msg.sum
		return m, sbAfter(30*time.Second, sbUsageTick{})
	case sbScanTick:
		return m, m.scanCmd()
	case sbUsageTick:
		return m, m.usageCmd()
	case sbClearMsg:
		m.status = ""
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
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
				return m.attach(m.items[m.cursor])
			}
		case "r":
			return m, m.scanCmd()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// attach ensures the selected devcontainer's pane and focuses it. The sidebar
// always runs inside the grid window, so focusing is just select-pane.
func (m sidebarModel) attach(c dcCandidate) (tea.Model, tea.Cmd) {
	if c.State != "running" {
		m.status = c.Name + " is " + c.State + " — run: weft dc " + c.Name + " --start"
		return m, sbAfter(5*time.Second, sbClearMsg{})
	}
	tm := tmux.New(m.r)
	paneID, err := dcShow(m.ctx, tm, c, dcLaunchArgs(c, false), true)
	if err != nil {
		m.status = err.Error()
		return m, sbAfter(5*time.Second, sbClearMsg{})
	}
	if paneID != "" {
		_ = tm.SelectPane(m.ctx, paneID)
	}
	m.status = "→ " + c.Name
	return m, tea.Batch(m.scanCmd(), sbAfter(3*time.Second, sbClearMsg{}))
}

func (m sidebarModel) View() tea.View {
	var b strings.Builder
	b.WriteString(colorize("weft", ansiCyan, true) + " " + colorize("devcontainers", ansiDim, true) + "\n\n")
	if len(m.items) == 0 {
		b.WriteString(colorize("  scanning…", ansiDim, true) + "\n")
	}
	for i, c := range m.items {
		glyph := colorize("○", ansiDim, true)
		if c.State == "running" {
			glyph = colorize("●", ansiGreen, true)
		}
		marker := ""
		switch {
		case c.PaneDead:
			marker = " " + colorize("✕", ansiRed, true)
		case c.Shown:
			marker = " " + colorize("▶", ansiCyan, true)
		case c.HasWindow:
			marker = " " + colorize("*", ansiDim, true)
		}
		name := truncate(c.Name, 20)
		prefix := "  "
		if i == m.cursor {
			prefix = colorize("❯ ", ansiCyan, true)
			name = colorize(name, ansiCyan, true)
		}
		b.WriteString(prefix + glyph + " " + name + marker + "\n")
	}

	t, w := m.sum.Today, m.sum.Week
	b.WriteString("\n" + colorize("usage", ansiDim, true) + "\n")
	fmt.Fprintf(&b, " today %s in · %s out\n", usage.Compact(t.InputTokens), usage.Compact(t.OutputTokens))
	fmt.Fprintf(&b, "       %d msgs · %d sess\n", t.Msgs, t.Sessions)
	fmt.Fprintf(&b, " 7d    %s in · %s out\n", usage.Compact(w.InputTokens), usage.Compact(w.OutputTokens))
	fmt.Fprintf(&b, "       %d msgs · %d sess\n", w.Msgs, w.Sessions)
	b.WriteString(colorize(" limits: /usage in claude", ansiDim, true) + "\n")

	if m.status != "" {
		b.WriteString("\n" + colorize(m.status, ansiYellow, true) + "\n")
	}
	b.WriteString("\n" + colorize("jk move · ⏎ attach · q quit", ansiDim, true) + "\n")
	return tea.NewView(b.String())
}
