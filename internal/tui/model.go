// Package tui implements weft's Bubble Tea dashboard.
package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/engine"
)

type mode int

const (
	modeList mode = iota
	modeNew
	modeConfirmDelete
	modeCreating
)

// Model is the dashboard's Bubble Tea model.
type Model struct {
	ctx    context.Context
	engine *engine.Engine
	theme  theme
	keys   keyMap

	table   table.Model
	input   textinput.Model
	spinner spinner.Model
	help    help.Model

	sessions []domain.Session
	width    int
	height   int

	mode         mode
	showHelp     bool
	toast        string
	deleteTarget string

	createCh     <-chan engine.Event
	createName   string
	createLogs   []string
	cancelCreate context.CancelFunc
}

// newProgram is a seam so tests can inject bubbletea IO options.
var newProgram = tea.NewProgram

// Run launches the dashboard.
func Run(ctx context.Context, e *engine.Engine) error {
	p := newProgram(newModel(ctx, e), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

func newModel(ctx context.Context, e *engine.Engine) Model {
	th := newTheme()

	tbl := table.New(table.WithFocused(true))

	ti := textinput.New()
	ti.Placeholder = "feat-auth"

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return Model{
		ctx:     ctx,
		engine:  e,
		theme:   th,
		keys:    newKeyMap(),
		table:   tbl,
		input:   ti,
		spinner: sp,
		help:    help.New(),
		mode:    modeList,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(reconcileCmd(m.ctx, m.engine), tickCmd())
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.setSize()
		return m, nil

	case tea.KeyPressMsg:
		switch m.mode {
		case modeNew:
			return m.updateNew(msg)
		case modeConfirmDelete:
			return m.updateConfirm(msg)
		case modeCreating:
			return m.updateCreating(msg)
		default:
			return m.updateList(msg)
		}

	case sessionsMsg:
		m.sessions = msg.sessions
		m.rebuildRows()
		return m, nil

	case sessionsErrMsg:
		m.toast = "reconcile: " + msg.err.Error()
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tickCmd()}
		if m.mode == modeList {
			cmds = append(cmds, reconcileCmd(m.ctx, m.engine))
		}
		return m, tea.Batch(cmds...)

	case createStartedMsg:
		m.createCh = msg.ch
		return m, tea.Batch(waitForEvent(msg.ch), m.spinner.Tick)

	case createEventMsg:
		switch msg.ev.Kind {
		case engine.EventStep:
			m.createLogs = appendLog(m.createLogs, "▶ "+msg.ev.Step)
		case engine.EventLog:
			m.createLogs = appendLog(m.createLogs, msg.ev.Text)
		}
		return m, waitForEvent(m.createCh)

	case createDoneMsg:
		m.mode = modeList
		m.cancelCreate = nil
		if msg.err != nil {
			m.toast = "create failed: " + msg.err.Error()
		} else {
			m.toast = "created " + m.createName
		}
		return m, reconcileCmd(m.ctx, m.engine)

	case actionDoneMsg:
		if msg.err != nil {
			m.toast = msg.err.Error()
		} else {
			m.toast = msg.note
		}
		return m, reconcileCmd(m.ctx, m.engine)

	case attachDoneMsg:
		if msg.err != nil {
			m.toast = "attach: " + msg.err.Error()
		}
		return m, reconcileCmd(m.ctx, m.engine)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.mode == modeCreating {
			return m, cmd
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		m.toast = ""
		return m, reconcileCmd(m.ctx, m.engine)
	case key.Matches(msg, m.keys.New):
		m.mode = modeNew
		m.input.SetValue("")
		return m, m.input.Focus()
	case key.Matches(msg, m.keys.Attach):
		if s, ok := m.selected(); ok {
			return m, attachCmd(m.ctx, m.engine, s.Name)
		}
		return m, nil
	case key.Matches(msg, m.keys.Stop):
		if s, ok := m.selected(); ok {
			m.toast = "stopping " + s.Name + "…"
			return m, stopCmd(m.ctx, m.engine, s.Name)
		}
		return m, nil
	case key.Matches(msg, m.keys.Start):
		if s, ok := m.selected(); ok {
			m.toast = "starting " + s.Name + "…"
			return m, startCmd(m.ctx, m.engine, s.Name)
		}
		return m, nil
	case key.Matches(msg, m.keys.Delete):
		if s, ok := m.selected(); ok {
			m.deleteTarget = s.Name
			m.mode = modeConfirmDelete
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
}

func (m Model) updateNew(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.mode = modeList
		return m, nil
	case msg.String() == "enter":
		name := strings.TrimSpace(m.input.Value())
		if !domain.ValidName(name) {
			m.toast = "invalid name (use letters, digits, . _ -)"
			return m, nil
		}
		cctx, cancel := context.WithCancel(m.ctx)
		m.cancelCreate = cancel
		m.createName = name
		m.createLogs = nil
		m.mode = modeCreating
		return m, startCreateCmd(cctx, m.engine, engine.NewSpec{Name: name})
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m Model) updateConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		name := m.deleteTarget
		m.mode = modeList
		m.toast = "removing " + name + "…"
		return m, removeCmd(m.ctx, m.engine, name)
	default:
		m.mode = modeList
		return m, nil
	}
}

func (m Model) updateCreating(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Cancel) && m.cancelCreate != nil {
		m.cancelCreate()
		m.toast = "cancelling…"
	}
	return m, nil
}

func (m *Model) setSize() {
	if m.width == 0 {
		return
	}
	const statusW, contW, claudeW = 12, 10, 9
	rest := m.width - (statusW + contW + claudeW) - 10
	if rest < 24 {
		rest = 24
	}
	nameW := rest / 2
	branchW := rest - nameW
	m.table.SetColumns([]table.Column{
		{Title: "STATUS", Width: statusW},
		{Title: "NAME", Width: nameW},
		{Title: "BRANCH", Width: branchW},
		{Title: "CONTAINER", Width: contW},
		{Title: "CLAUDE", Width: claudeW},
	})
	m.table.SetWidth(m.width)
	h := m.height - 6
	if h < 3 {
		h = 3
	}
	m.table.SetHeight(h)
}

func (m *Model) rebuildRows() {
	rows := make([]table.Row, len(m.sessions))
	for i, s := range m.sessions {
		rows[i] = table.Row{
			m.theme.statusCell(s.Status),
			s.Name,
			s.Branch,
			containerCell(s),
			claudeCell(s.Claude),
		}
	}
	m.table.SetRows(rows)
}

func (m Model) selected() (domain.Session, bool) {
	i := m.table.Cursor()
	if i >= 0 && i < len(m.sessions) {
		return m.sessions[i], true
	}
	return domain.Session{}, false
}

func appendLog(logs []string, line string) []string {
	logs = append(logs, line)
	const max = 200
	if len(logs) > max {
		logs = logs[len(logs)-max:]
	}
	return logs
}
