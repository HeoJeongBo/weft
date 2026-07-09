// Package sysexec is the single os/exec abstraction every external-tool wrapper
// depends on. It centralizes command execution, streaming, dry-run handling, and
// error shaping so the wrappers stay small and testable.
package sysexec

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// StdStream identifies which stream a streamed line came from.
type StdStream int

// Stream identifiers for streamed output lines.
const (
	StreamStdout StdStream = iota
	StreamStderr
)

// Line is a single line emitted during a streamed command.
type Line struct {
	Stream StdStream
	Text   string
}

// Result holds the captured output of a command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner executes external commands. Reads use Run; mutations use Mutate (which
// is skipped under dry-run); long-running mutations that stream progress use
// Stream. Look reports a binary's path.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
	Mutate(ctx context.Context, name string, args ...string) (Result, error)
	Stream(ctx context.Context, sink func(Line), name string, args ...string) (Result, error)
	Look(name string) (string, error)
	DryRun() bool
}

// Exec is the real Runner backed by os/exec.
type Exec struct {
	dryRun bool
	Log    *slog.Logger
}

// New returns an Exec runner. A nil logger falls back to slog.Default.
func New(dryRun bool, log *slog.Logger) *Exec {
	if log == nil {
		log = slog.Default()
	}
	return &Exec{dryRun: dryRun, Log: log}
}

// DryRun reports whether mutations are skipped.
func (e *Exec) DryRun() bool { return e.dryRun }

// Look reports the absolute path of a binary on PATH.
func (e *Exec) Look(name string) (string, error) { return exec.LookPath(name) }

// Run executes a read-only command, capturing output. It always runs, even under
// dry-run, because reads have no side effects.
func (e *Exec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return e.capture(ctx, false, name, args...)
}

// Mutate executes a state-changing command. Under dry-run it logs the argv and
// returns an empty successful result instead of executing.
func (e *Exec) Mutate(ctx context.Context, name string, args ...string) (Result, error) {
	return e.capture(ctx, true, name, args...)
}

func (e *Exec) capture(ctx context.Context, mutate bool, name string, args ...string) (Result, error) {
	if mutate && e.dryRun {
		e.Log.Info("dry-run", "cmd", cmdline(name, args))
		return Result{}, nil
	}
	e.Log.Debug("exec", "cmd", cmdline(name, args))

	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()

	res := Result{Stdout: out.String(), Stderr: errb.String()}
	if err != nil {
		res.ExitCode = exitCode(err)
		return res, cmdErr(name, args, res.ExitCode, errb.String(), err)
	}
	return res, nil
}

// Stream runs a state-changing command, forwarding each output line to sink as it
// arrives, while also capturing the full output. Under dry-run it logs and skips.
// A nil sink is allowed (behaves like Mutate but line-buffered).
func (e *Exec) Stream(ctx context.Context, sink func(Line), name string, args ...string) (Result, error) {
	if e.dryRun {
		e.Log.Info("dry-run", "cmd", cmdline(name, args))
		return Result{}, nil
	}
	e.Log.Debug("stream", "cmd", cmdline(name, args))

	cmd := exec.CommandContext(ctx, name, args...)
	outw := &lineWriter{stream: StreamStdout, sink: sink}
	errw := &lineWriter{stream: StreamStderr, sink: sink}
	cmd.Stdout = outw
	cmd.Stderr = errw

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	waitErr := cmd.Wait()
	outw.flush()
	errw.flush()

	res := Result{Stdout: outw.buf.String(), Stderr: errw.buf.String()}
	if waitErr != nil {
		res.ExitCode = exitCode(waitErr)
		return res, cmdErr(name, args, res.ExitCode, errw.buf.String(), waitErr)
	}
	return res, nil
}

// lineWriter captures all bytes written to it and, when a sink is set, forwards
// each complete line as it arrives. Used as cmd.Stdout/cmd.Stderr for Stream.
type lineWriter struct {
	stream  StdStream
	sink    func(Line)
	buf     bytes.Buffer
	pending []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.sink != nil {
		w.pending = append(w.pending, p...)
		for {
			i := bytes.IndexByte(w.pending, '\n')
			if i < 0 {
				break
			}
			w.sink(Line{Stream: w.stream, Text: string(w.pending[:i])})
			w.pending = w.pending[i+1:]
		}
	}
	return len(p), nil
}

// flush emits any trailing partial line (no terminating newline).
func (w *lineWriter) flush() {
	if w.sink != nil && len(w.pending) > 0 {
		w.sink(Line{Stream: w.stream, Text: string(w.pending)})
		w.pending = nil
	}
}

func cmdErr(name string, args []string, code int, stderr string, cause error) error {
	// Preserve context cancellation so callers can distinguish it.
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	return &wefterr.CmdError{Cmd: name, Args: args, ExitCode: code, Stderr: stderr}
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func cmdline(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}
