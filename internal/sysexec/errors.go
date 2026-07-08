package sysexec

import (
	"errors"

	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// IsCommandError reports whether err is a non-zero-exit command error (as opposed
// to a missing binary, context cancellation, or other failure).
func IsCommandError(err error) bool {
	var ce *wefterr.CmdError
	return errors.As(err, &ce)
}

// CommandExitCode returns the process exit code carried by err, and whether err
// was a command error at all. A missing binary yields code -1.
func CommandExitCode(err error) (int, bool) {
	var ce *wefterr.CmdError
	if errors.As(err, &ce) {
		return ce.ExitCode, true
	}
	return 0, false
}
