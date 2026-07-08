// Package wefterr defines weft's sentinel errors, a command-error type, and the
// mapping from errors to process exit codes.
package wefterr

import (
	"errors"
	"fmt"
	"strings"
)

// Exit codes returned by the weft binary.
const (
	CodeOK         = 0
	CodeGeneric    = 1
	CodeUsage      = 2
	CodeDependency = 3 // a preflight / dependency check failed
	CodeDataLoss   = 4 // aborted to avoid destroying unsaved work
)

// Sentinel errors. Compare with errors.Is.
var (
	ErrNotInRepo           = errors.New("not inside a git repository")
	ErrSessionExists       = errors.New("session already exists")
	ErrSessionNotFound     = errors.New("session not found")
	ErrDirtyWorktree       = errors.New("worktree has uncommitted or unpushed work")
	ErrDevcontainerMissing = errors.New("no devcontainer configuration found")
	ErrDependencyMissing   = errors.New("a required dependency is missing")
)

// CmdError describes a failed external command invocation.
type CmdError struct {
	Cmd      string
	Args     []string
	ExitCode int
	Stderr   string
}

func (e *CmdError) Error() string {
	msg := fmt.Sprintf("%s %s: exit %d", e.Cmd, strings.Join(e.Args, " "), e.ExitCode)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + firstLine(s)
	}
	return msg
}

// coded wraps an error with an explicit exit code.
type coded struct {
	err  error
	code int
}

func (c *coded) Error() string { return c.err.Error() }
func (c *coded) Unwrap() error { return c.err }

// WithExitCode annotates err with an explicit exit code.
func WithExitCode(err error, code int) error {
	if err == nil {
		return nil
	}
	return &coded{err: err, code: code}
}

// ExitCode returns the process exit code that best represents err.
func ExitCode(err error) int {
	if err == nil {
		return CodeOK
	}
	var c *coded
	if errors.As(err, &c) {
		return c.code
	}
	switch {
	case errors.Is(err, ErrDependencyMissing):
		return CodeDependency
	case errors.Is(err, ErrDirtyWorktree):
		return CodeDataLoss
	default:
		return CodeGeneric
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
