package tui

import (
	"context"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/HeoJeongBo/weft/internal/domain"
	"github.com/HeoJeongBo/weft/internal/engine"
	"github.com/HeoJeongBo/weft/internal/tmux"
)

const pollInterval = 2500 * time.Millisecond

// Messages.
type (
	sessionsMsg      struct{ sessions []domain.Session }
	sessionsErrMsg   struct{ err error }
	tickMsg          time.Time
	createStartedMsg struct{ ch <-chan engine.Event }
	createEventMsg   struct{ ev engine.Event }
	createDoneMsg    struct{ err error }
	actionDoneMsg    struct {
		note string
		err  error
	}
	attachDoneMsg struct{ err error }
)

func reconcileCmd(ctx context.Context, e *engine.Engine) tea.Cmd {
	return func() tea.Msg {
		ss, err := e.Reconcile(ctx)
		if err != nil {
			return sessionsErrMsg{err}
		}
		return sessionsMsg{ss}
	}
}

func toTickMsg(t time.Time) tea.Msg { return tickMsg(t) }

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, toTickMsg)
}

func startCreateCmd(ctx context.Context, e *engine.Engine, spec engine.NewSpec) tea.Cmd {
	return func() tea.Msg {
		return createStartedMsg{ch: e.CreateSession(ctx, spec)}
	}
}

func waitForEvent(ch <-chan engine.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return createDoneMsg{}
		}
		if ev.Kind == engine.EventError {
			return createDoneMsg{err: ev.Err}
		}
		return createEventMsg{ev}
	}
}

func stopCmd(ctx context.Context, e *engine.Engine, name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{note: "stopped " + name, err: e.Stop(ctx, name, nil)}
	}
}

func startCmd(ctx context.Context, e *engine.Engine, name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{note: "started " + name, err: e.Start(ctx, name, false, nil)}
	}
}

func removeCmd(ctx context.Context, e *engine.Engine, name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{note: "removed " + name, err: e.Remove(ctx, engine.RemoveSpec{Name: name}, nil)}
	}
}

// attachCmd focuses the session's window and hands off. Inside tmux it switches
// the client (non-blocking); outside tmux it blocks via ExecProcess and returns
// when the user detaches.
func attachCmd(ctx context.Context, e *engine.Engine, name string) tea.Cmd {
	target := e.Project.WindowTarget(name)
	_ = e.Tmux.SelectWindow(ctx, target)

	if tmux.InTmux() {
		return func() tea.Msg {
			return attachResult(e.Tmux.SwitchClient(ctx, target))
		}
	}
	c := exec.CommandContext(ctx, "tmux", tmux.AttachArgs(e.Project.TmuxSession)...)
	return tea.ExecProcess(c, attachResult)
}

// attachResult maps an attach's error into the message the model expects.
func attachResult(err error) tea.Msg { return attachDoneMsg{err} }
