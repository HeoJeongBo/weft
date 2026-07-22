package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/devcontainer"
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
	sbScanMsg struct {
		cands []dcCandidate
		err   error
	}
	sbUsageMsg  struct{ sum usage.Summary }
	sbScanTick  struct{}
	sbUsageTick struct{}
	sbClearMsg  struct{}
	sbUpDoneMsg struct {
		cand dcCandidate
		err  error
	}
)

// sidebarModel is the Orca-style left rail: the devcontainer list on top and a
// usage summary below, refreshed on ticks.
type sidebarModel struct {
	ctx    context.Context
	r      sysexec.Runner
	items  []dcCandidate
	cursor int
	selKey string // identity of the item under the cursor (survives re-sorts)
	sum    usage.Summary
	status string
	width  int
	height int
}

func dcKey(c dcCandidate) string { return c.Folder + "\x00" + c.ConfigPath }

func newSidebarModel(ctx context.Context, r sysexec.Runner) sidebarModel {
	return sidebarModel{ctx: ctx, r: r}
}

func (m sidebarModel) Init() tea.Cmd {
	return tea.Batch(m.scanCmd(), m.usageCmd())
}

func (m sidebarModel) scanCmd() tea.Cmd {
	return func() tea.Msg {
		cands, err := dcScan(m.ctx, m.r)
		return sbScanMsg{cands: cands, err: err}
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
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sbScanMsg:
		if msg.err != nil {
			// Keep showing the last-known list; vanishing entries would read
			// as "my devcontainers are gone" when docker just hiccuped.
			m.status = "docker unreachable"
			return m, sbAfter(3*time.Second, sbScanTick{})
		}
		if m.status == "docker unreachable" {
			m.status = ""
		}
		m.items = msg.cands
		// The list re-sorts every scan: keep the cursor on the same item by
		// identity; on the first scan land on the displayed (▶) one.
		if m.selKey == "" {
			for i, c := range m.items {
				if c.Shown {
					m.cursor = i
					m.selKey = dcKey(c)
				}
			}
		} else {
			for i, c := range m.items {
				if dcKey(c) == m.selKey {
					m.cursor = i
				}
			}
		}
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
	case sbUpDoneMsg:
		if msg.err != nil {
			m.status = "start failed: " + msg.err.Error()
			return m, tea.Batch(m.scanCmd(), sbAfter(5*time.Second, sbClearMsg{}))
		}
		started := msg.cand
		started.State = "running"
		return m.attach(started)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.selKey = dcKey(m.items[m.cursor])
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				m.selKey = dcKey(m.items[m.cursor])
			}
		case "enter":
			if len(m.items) > 0 {
				return m.attach(m.items[m.cursor])
			}
		case "x":
			if len(m.items) > 0 {
				return m.close(m.items[m.cursor])
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
// always runs inside the grid window, so focusing is just select-pane. A
// stopped devcontainer is brought up first (asynchronously — up can take a
// while) and attached when done.
func (m sidebarModel) attach(c dcCandidate) (tea.Model, tea.Cmd) {
	if c.State != "running" {
		m.status = "starting " + c.Label + "…"
		up := func() tea.Msg {
			_, err := devcontainer.New(m.r).Up(m.ctx, nil, devcontainer.UpOpts{
				WorkspaceFolder: c.Folder,
				ConfigPath:      c.ConfigPath,
			})
			return sbUpDoneMsg{cand: c, err: err}
		}
		return m, up
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
	m.status = "→ " + c.Label
	return m, tea.Batch(m.scanCmd(), sbAfter(3*time.Second, sbClearMsg{}))
}

// close kills the selected devcontainer's pane — shown or parked — so an
// accidentally attached claude can be dismissed from the list.
func (m sidebarModel) close(c dcCandidate) (tea.Model, tea.Cmd) {
	if !c.HasWindow {
		m.status = c.Name + " has no pane"
		return m, sbAfter(3*time.Second, sbClearMsg{})
	}
	tm := tmux.New(m.r)
	all, _ := tm.ListAllPanes(m.ctx, dcTmuxSession)
	id := dcFindPane(all, c)
	switch {
	case id == "":
		m.status = c.Name + " pane already gone"
	case tm.KillPane(m.ctx, id) != nil:
		m.status = "could not close " + c.Name
	default:
		m.status = "✕ " + c.Name
	}
	return m, tea.Batch(m.scanCmd(), sbAfter(3*time.Second, sbClearMsg{}))
}

// listWindow returns the visible slice bounds of the item list given the pane
// height: the header and the usage/hint tail are always reserved, and the
// window slides to keep the cursor visible.
func (m sidebarModel) listWindow() (start, end int) {
	const reserved = 2 + 6 + 3 + 2 // header, usage block, hints, status slack
	avail := len(m.items)
	if m.height > 0 {
		if v := m.height - reserved; v < avail {
			avail = max(3, v)
		}
	}
	if avail >= len(m.items) {
		return 0, len(m.items)
	}
	start = m.cursor - avail/2
	start = min(max(start, 0), len(m.items)-avail)
	return start, start + avail
}

func (m sidebarModel) nameWidth() int {
	if m.width > 10 {
		return min(m.width-8, 40)
	}
	return 20
}

func (m sidebarModel) View() tea.View {
	var b strings.Builder
	b.WriteString(colorize("weft", ansiCyan, true) + " " + colorize("devcontainers", ansiDim, true) + "\n\n")
	if len(m.items) == 0 {
		b.WriteString(colorize("  scanning…", ansiDim, true) + "\n")
	}
	start, end := m.listWindow()
	if start > 0 {
		b.WriteString(colorize(fmt.Sprintf("  ↑ %d more", start), ansiDim, true) + "\n")
	}
	for i := start; i < end; i++ {
		c := m.items[i]
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
		if !c.PaneDead && c.HasWindow && strings.Contains(c.PaneTitle, "✳") {
			// claude rewrites the pane title while it is working.
			marker += colorize("✳", ansiYellow, true)
		}
		name := truncate(c.Label, m.nameWidth())
		prefix := "  "
		if i == m.cursor {
			prefix = colorize("❯ ", ansiCyan, true)
			name = colorize(name, ansiCyan, true)
		}
		b.WriteString(prefix + glyph + " " + name + marker + "\n")
	}
	if end < len(m.items) {
		b.WriteString(colorize(fmt.Sprintf("  ↓ %d more", len(m.items)-end), ansiDim, true) + "\n")
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
	b.WriteString("\n" + colorize("↑↓ move · ⏎ attach · x close", ansiDim, true) + "\n")
	b.WriteString(colorize("^B d leave · ✳ working", ansiDim, true) + "\n")
	return tea.NewView(b.String())
}
