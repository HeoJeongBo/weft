// Package tmux wraps the tmux CLI for weft's session/window model: one tmux
// session per project, one window per weft session.
//
// Interactive attach is intentionally NOT a Runner method — it must inherit the
// terminal. Callers build the command from AttachArgs and run it themselves
// (the CLI with inherited stdio, the TUI via tea.ExecProcess).
package tmux

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

// Window is one entry of `tmux list-windows`.
type Window struct {
	ID          string // "@N"
	Name        string
	Index       int
	Active      bool
	PaneCommand string // active pane's foreground command
	PaneDead    bool
	Activity    int64 // window_activity, unix seconds
}

// Pane is one entry of `tmux list-panes`. StartCommand is the command the pane
// was created with — a stable identifier that survives programs rewriting the
// pane title.
type Pane struct {
	ID           string // "%N"
	WindowID     string // "@N"
	Dead         bool
	Command      string // current foreground command
	StartCommand string
}

// Tmux is the subset of tmux operations weft depends on.
type Tmux interface {
	HasSession(ctx context.Context, session string) (bool, error)
	NewSession(ctx context.Context, session, startDir string) error
	NewWindow(ctx context.Context, session, name, startDir string, cmd []string) (id string, err error)
	ListWindows(ctx context.Context, session string) ([]Window, error)
	ListPanes(ctx context.Context, target string) ([]Pane, error)
	ListAllPanes(ctx context.Context, session string) ([]Pane, error)
	SplitWindowLeft(ctx context.Context, target string, width int, cmd []string) (id string, err error)
	SplitWindowRight(ctx context.Context, target, startDir string, cmd []string) (id string, err error)
	NewBackgroundWindow(ctx context.Context, session, name, startDir string, cmd []string) (paneID string, err error)
	SwapPane(ctx context.Context, src, dst string) error
	JoinPaneRight(ctx context.Context, src, dst string) error
	BreakPane(ctx context.Context, src string) error
	KillWindow(ctx context.Context, target string) error
	KillSession(ctx context.Context, session string) error
	SelectWindow(ctx context.Context, target string) error
	SelectPane(ctx context.Context, target string) error
	RenameWindow(ctx context.Context, target, name string) error
	SwitchClient(ctx context.Context, target string) error
	SendKeys(ctx context.Context, target string, keys ...string) error
}

// Exec is the real Tmux backed by a sysexec.Runner.
type Exec struct {
	r sysexec.Runner
}

// New returns a Tmux backed by r.
func New(r sysexec.Runner) *Exec { return &Exec{r: r} }

// InTmux reports whether the current process is running inside tmux.
func InTmux() bool { return os.Getenv("TMUX") != "" }

// AttachArgs builds the argv to attach the current terminal to a session.
func AttachArgs(session string) []string { return []string{"attach-session", "-t", session} }

// HasSession reports whether the session exists.
func (e *Exec) HasSession(ctx context.Context, session string) (bool, error) {
	_, err := e.r.Run(ctx, "tmux", "has-session", "-t", session)
	if err == nil {
		return true, nil
	}
	// has-session exits 1 when the session (or server) is absent; a missing
	// binary or other failure (exit -1) propagates.
	if code, ok := sysexec.CommandExitCode(err); ok && code == 1 {
		return false, nil
	}
	return false, err
}

// NewSession creates a detached session rooted at startDir.
func (e *Exec) NewSession(ctx context.Context, session, startDir string) error {
	args := []string{"new-session", "-d", "-s", session}
	if startDir != "" {
		args = append(args, "-c", startDir)
	}
	_, err := e.r.Mutate(ctx, "tmux", args...)
	return err
}

// NewWindow creates a window in session and returns its window id ("@N"). When
// cmd is non-empty it becomes the window's foreground command.
func (e *Exec) NewWindow(ctx context.Context, session, name, startDir string, cmd []string) (string, error) {
	args := []string{"new-window", "-t", session + ":", "-n", name, "-P", "-F", "#{window_id}"}
	if startDir != "" {
		args = append(args, "-c", startDir)
	}
	if len(cmd) > 0 {
		args = append(args, "--")
		args = append(args, cmd...)
	}
	res, err := e.r.Mutate(ctx, "tmux", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ListWindows lists windows in session. It returns an empty slice when the
// session does not exist.
func (e *Exec) ListWindows(ctx context.Context, session string) ([]Window, error) {
	const format = "#{window_id}\t#{window_name}\t#{window_index}\t#{window_active}\t#{pane_current_command}\t#{pane_dead}\t#{window_activity}"
	res, err := e.r.Run(ctx, "tmux", "list-windows", "-t", session, "-F", format)
	if err != nil {
		if code, ok := sysexec.CommandExitCode(err); ok && code == 1 {
			return nil, nil // session/server absent
		}
		return nil, err
	}
	return parseWindows(res.Stdout), nil
}

// paneFormat is shared by the pane listings; the start command comes last so
// embedded tabs cannot shift the fixed fields.
const paneFormat = "#{pane_id}\t#{window_id}\t#{pane_dead}\t#{pane_current_command}\t#{pane_start_command}"

// ListPanes lists panes of the target window. It returns an empty slice when
// the session or window does not exist.
func (e *Exec) ListPanes(ctx context.Context, target string) ([]Pane, error) {
	return e.listPanes(ctx, "-t", target)
}

// ListAllPanes lists every pane in the session, across all windows — including
// background "parked" ones.
func (e *Exec) ListAllPanes(ctx context.Context, session string) ([]Pane, error) {
	return e.listPanes(ctx, "-s", "-t", session)
}

func (e *Exec) listPanes(ctx context.Context, sel ...string) ([]Pane, error) {
	args := append([]string{"list-panes"}, sel...)
	res, err := e.r.Run(ctx, "tmux", append(args, "-F", paneFormat)...)
	if err != nil {
		if code, ok := sysexec.CommandExitCode(err); ok && code == 1 {
			return nil, nil // session/window absent
		}
		return nil, err
	}
	return parsePanes(res.Stdout), nil
}

// SplitWindowLeft splits the target window with a fixed-width pane on the far
// left (a sidebar) and returns the new pane id.
func (e *Exec) SplitWindowLeft(ctx context.Context, target string, width int, cmd []string) (string, error) {
	return e.splitWindow(ctx, cmd, "-hbf", "-l", strconv.Itoa(width), "-t", target)
}

// SplitWindowRight splits the target horizontally (new pane on the right) and
// returns the new pane id.
func (e *Exec) SplitWindowRight(ctx context.Context, target, startDir string, cmd []string) (string, error) {
	args := []string{"-h", "-t", target}
	if startDir != "" {
		args = append(args, "-c", startDir)
	}
	return e.splitWindow(ctx, cmd, args...)
}

func (e *Exec) splitWindow(ctx context.Context, cmd []string, opts ...string) (string, error) {
	args := append([]string{"split-window"}, opts...)
	args = append(args, "-P", "-F", "#{pane_id}")
	if len(cmd) > 0 {
		args = append(args, "--")
		args = append(args, cmd...)
	}
	res, err := e.r.Mutate(ctx, "tmux", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// NewBackgroundWindow creates a detached window running cmd and returns the id
// of its pane — a "parked" pane ready to be swapped into view.
func (e *Exec) NewBackgroundWindow(ctx context.Context, session, name, startDir string, cmd []string) (string, error) {
	args := []string{"new-window", "-d", "-t", session + ":", "-n", name, "-P", "-F", "#{pane_id}"}
	if startDir != "" {
		args = append(args, "-c", startDir)
	}
	if len(cmd) > 0 {
		args = append(args, "--")
		args = append(args, cmd...)
	}
	res, err := e.r.Mutate(ctx, "tmux", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// SwapPane exchanges the positions of two panes (they may live in different
// windows) without focusing either.
func (e *Exec) SwapPane(ctx context.Context, src, dst string) error {
	_, err := e.r.Mutate(ctx, "tmux", "swap-pane", "-d", "-s", src, "-t", dst)
	return err
}

// JoinPaneRight moves the src pane into dst as a new pane on the right.
func (e *Exec) JoinPaneRight(ctx context.Context, src, dst string) error {
	_, err := e.r.Mutate(ctx, "tmux", "join-pane", "-h", "-s", src, "-t", dst)
	return err
}

// BreakPane detaches src into its own (background) window.
func (e *Exec) BreakPane(ctx context.Context, src string) error {
	_, err := e.r.Mutate(ctx, "tmux", "break-pane", "-d", "-s", src)
	return err
}

// SelectPane focuses the pane identified by target ("%id" or full target).
func (e *Exec) SelectPane(ctx context.Context, target string) error {
	_, err := e.r.Mutate(ctx, "tmux", "select-pane", "-t", target)
	return err
}

// RenameWindow renames the target window.
func (e *Exec) RenameWindow(ctx context.Context, target, name string) error {
	_, err := e.r.Mutate(ctx, "tmux", "rename-window", "-t", target, name)
	return err
}

// KillWindow kills the window identified by target ("session:window" or "@id").
func (e *Exec) KillWindow(ctx context.Context, target string) error {
	_, err := e.r.Mutate(ctx, "tmux", "kill-window", "-t", target)
	return err
}

// KillSession kills the whole session.
func (e *Exec) KillSession(ctx context.Context, session string) error {
	_, err := e.r.Mutate(ctx, "tmux", "kill-session", "-t", session)
	return err
}

// SelectWindow focuses target within its session (no client required).
func (e *Exec) SelectWindow(ctx context.Context, target string) error {
	_, err := e.r.Mutate(ctx, "tmux", "select-window", "-t", target)
	return err
}

// SwitchClient moves the attached client to target. Non-blocking; used when
// already inside tmux (where attach must not nest).
func (e *Exec) SwitchClient(ctx context.Context, target string) error {
	_, err := e.r.Mutate(ctx, "tmux", "switch-client", "-t", target)
	return err
}

// SendKeys sends key(s) to target (e.g. "C-c").
func (e *Exec) SendKeys(ctx context.Context, target string, keys ...string) error {
	args := append([]string{"send-keys", "-t", target}, keys...)
	_, err := e.r.Mutate(ctx, "tmux", args...)
	return err
}

func parsePanes(out string) []Pane {
	var ps []Pane
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 5)
		if len(f) < 5 {
			continue
		}
		ps = append(ps, Pane{ID: f[0], WindowID: f[1], Dead: f[2] == "1", Command: f[3], StartCommand: f[4]})
	}
	return ps
}

func parseWindows(out string) []Window {
	var ws []Window
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		idx, _ := strconv.Atoi(f[2])
		act, _ := strconv.ParseInt(f[6], 10, 64)
		ws = append(ws, Window{
			ID:          f[0],
			Name:        f[1],
			Index:       idx,
			Active:      f[3] == "1",
			PaneCommand: f[4],
			PaneDead:    f[5] == "1",
			Activity:    act,
		})
	}
	return ws
}
