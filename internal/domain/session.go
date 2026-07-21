// Package domain holds weft's core types. It performs no I/O and imports no
// wrapper packages, so it can be reasoned about and tested in isolation.
package domain

import "time"

// SessionStatus is a session's derived, at-a-glance state.
type SessionStatus string

// Session statuses.
const (
	StatusReady    SessionStatus = "ready"    // worktree + window + running container
	StatusStarting SessionStatus = "starting" // container created/restarting, not yet running
	StatusDetached SessionStatus = "detached" // container running but window missing/dead
	StatusStopped  SessionStatus = "stopped"  // worktree present, container down/absent
	StatusPartial  SessionStatus = "partial"  // worktree present but window & container missing
	StatusOrphaned SessionStatus = "orphaned" // container/window with no worktree
	StatusUnknown  SessionStatus = "unknown"
)

// ClaudeState describes what the session's tmux window is running.
type ClaudeState string

// Claude pane states.
const (
	ClaudeRunning ClaudeState = "running"
	ClaudeIdle    ClaudeState = "idle"
	ClaudeDead    ClaudeState = "dead"
	ClaudeNone    ClaudeState = "none"
)

// Worktree is a session's git worktree.
type Worktree struct {
	Path     string
	Branch   string
	Head     string
	Dirty    bool
	Ahead    int
	Behind   int
	Locked   bool
	Prunable bool
}

// Container is a session's devcontainer.
type Container struct {
	ID                    string
	Image                 string
	State                 string // running, exited, created, paused, ...
	RemoteUser            string
	RemoteWorkspaceFolder string
}

// Running reports whether the container is up.
func (c *Container) Running() bool { return c != nil && c.State == "running" }

// Window is a session's tmux window.
type Window struct {
	ID          string
	Index       int
	Name        string
	Active      bool
	PaneCommand string
	PaneDead    bool
	Activity    int64 // unix seconds of last activity
}

// Session is the aggregate root, keyed by Name within a project.
type Session struct {
	Name      string
	Project   string // project slug
	Branch    string
	BaseRef   string
	CreatedAt time.Time

	Worktree  *Worktree
	Container *Container
	Window    *Window

	Status SessionStatus
	Claude ClaudeState
}

// Key returns the "<project>/<name>" correlation key.
func (s *Session) Key() string { return SessionKey(s.Project, s.Name) }

// DeriveStatus computes Status and Claude from the currently-populated pieces.
// devcontainerExpected reports whether this session is supposed to have a
// container (project/session config): when false, a live tmux window alone means
// the session is Ready. claudeCmds are the process names that count as a live
// claude (e.g. "claude", "node").
func (s *Session) DeriveStatus(devcontainerExpected bool, claudeCmds ...string) {
	s.Claude = deriveClaude(s.Window, claudeCmds)

	hasWT := s.Worktree != nil
	hasWin := s.Window != nil && !s.Window.PaneDead
	running := s.Container.Running()

	switch {
	case !hasWT:
		// A container or window with no worktree is an orphan to clean up.
		s.Status = StatusOrphaned
	case !devcontainerExpected:
		// tmux-only session: the container is irrelevant. A live window is Ready;
		// otherwise the worktree remains but the session isn't running.
		if hasWin {
			s.Status = StatusReady
		} else {
			s.Status = StatusStopped
		}
	case hasWin && running:
		s.Status = StatusReady
	case running:
		// Container is up but its window is gone — `weft start` recreates it.
		s.Status = StatusDetached
	case s.Container != nil && (s.Container.State == "created" || s.Container.State == "restarting"):
		s.Status = StatusStarting
	case hasWin || s.Container != nil:
		// Worktree plus at least one of window/container, but not running.
		s.Status = StatusStopped
	default:
		// Worktree only — a partial or freshly-created session.
		s.Status = StatusPartial
	}
}

func deriveClaude(w *Window, claudeCmds []string) ClaudeState {
	if w == nil {
		return ClaudeNone
	}
	if w.PaneDead {
		return ClaudeDead
	}
	for _, c := range claudeCmds {
		if w.PaneCommand == c {
			return ClaudeRunning
		}
	}
	return ClaudeIdle
}
