package sysexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func quietExec(dryRun bool) *Exec {
	return New(dryRun, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunCapturesStreams(t *testing.T) {
	e := quietExec(false)
	res, err := e.Run(context.Background(), "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "out\n" || res.Stderr != "err\n" {
		t.Errorf("stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}

func TestRunExitCode(t *testing.T) {
	e := quietExec(false)
	_, err := e.Run(context.Background(), "sh", "-c", "exit 3")
	code, ok := CommandExitCode(err)
	if !ok || code != 3 {
		t.Fatalf("want exit 3 command error, got code=%d ok=%v err=%v", code, ok, err)
	}
	var ce *wefterr.CmdError
	if !errors.As(err, &ce) {
		t.Errorf("want CmdError, got %T", err)
	}
}

func TestStreamForwardsLines(t *testing.T) {
	e := quietExec(false)
	var got []string
	res, err := e.Stream(context.Background(), func(l Line) {
		if l.Stream == StreamStdout {
			got = append(got, l.Text)
		}
	}, "sh", "-c", "echo a; echo b; echo c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("streamed lines = %v", got)
	}
	if res.Stdout != "a\nb\nc\n" {
		t.Errorf("captured stdout = %q", res.Stdout)
	}
}

func TestDryRunSkipsMutate(t *testing.T) {
	e := quietExec(true)
	// Would exit 1 if actually run; dry-run must skip and succeed.
	res, err := e.Mutate(context.Background(), "sh", "-c", "exit 1")
	if err != nil {
		t.Fatalf("dry-run mutate should not error, got %v", err)
	}
	if res.Stdout != "" {
		t.Errorf("dry-run should produce no output, got %q", res.Stdout)
	}

	// Run (read) still executes under dry-run.
	res, err = e.Run(context.Background(), "sh", "-c", "echo real")
	if err != nil || res.Stdout != "real\n" {
		t.Errorf("read under dry-run should execute: out=%q err=%v", res.Stdout, err)
	}
}

func TestLook(t *testing.T) {
	e := quietExec(false)
	if _, err := e.Look("sh"); err != nil {
		t.Errorf("expected to find sh: %v", err)
	}
	if _, err := e.Look("weft-nonexistent-binary-xyz"); err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestIsCommandError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"cmd error", &wefterr.CmdError{Cmd: "git", ExitCode: 1}, true},
		{"wrapped cmd error", fmt.Errorf("wrap: %w", &wefterr.CmdError{Cmd: "git"}), true},
		{"plain error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCommandError(tc.err); got != tc.want {
				t.Errorf("IsCommandError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestCommandExitCode(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantOK   bool
	}{
		{"cmd error", &wefterr.CmdError{ExitCode: 7}, 7, true},
		{"missing binary code", &wefterr.CmdError{ExitCode: -1}, -1, true},
		{"plain error", errors.New("boom"), 0, false},
		{"nil", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, ok := CommandExitCode(tc.err)
			if code != tc.wantCode || ok != tc.wantOK {
				t.Errorf("CommandExitCode(%v) = (%d,%v), want (%d,%v)", tc.err, code, ok, tc.wantCode, tc.wantOK)
			}
		})
	}
}

// A missing binary produces a command error carrying exit code -1 (not an
// *exec.ExitError), exercising the exitCode fallback branch.
func TestRunMissingBinaryExitCodeMinusOne(t *testing.T) {
	e := quietExec(false)
	_, err := e.Run(context.Background(), "weft-nonexistent-binary-xyz")
	code, ok := CommandExitCode(err)
	if !ok || code != -1 {
		t.Fatalf("want (-1,true), got (%d,%v) err=%v", code, ok, err)
	}
	if !IsCommandError(err) {
		t.Errorf("missing binary should be a command error, got %T", err)
	}
}

// A command with no args exercises the cmdline no-args branch.
func TestRunNoArgs(t *testing.T) {
	e := quietExec(false)
	if _, err := e.Run(context.Background(), "true"); err != nil {
		t.Fatalf("true should succeed: %v", err)
	}
}

func TestNewNilLoggerDefaults(t *testing.T) {
	e := New(false, nil)
	if e.Log == nil {
		t.Fatal("nil logger should fall back to a default, got nil")
	}
}

func TestDryRunReports(t *testing.T) {
	if !New(true, nil).DryRun() {
		t.Error("New(true).DryRun() should be true")
	}
	if New(false, nil).DryRun() {
		t.Error("New(false).DryRun() should be false")
	}
}

func TestStreamTrailingPartialLine(t *testing.T) {
	e := quietExec(false)
	var got []string
	res, err := e.Stream(context.Background(), func(l Line) {
		got = append(got, l.Text)
	}, "printf", "x")
	if err != nil {
		t.Fatal(err)
	}
	// printf 'x' emits no trailing newline; flush must still deliver "x".
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("streamed lines = %v, want [x]", got)
	}
	if res.Stdout != "x" {
		t.Errorf("captured stdout = %q, want %q", res.Stdout, "x")
	}
}

func TestStreamNilSink(t *testing.T) {
	e := quietExec(false)
	res, err := e.Stream(context.Background(), nil, "sh", "-c", "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "hi\n" {
		t.Errorf("captured stdout = %q, want %q", res.Stdout, "hi\n")
	}
}

func TestStreamStderrCapture(t *testing.T) {
	e := quietExec(false)
	var errLines []string
	res, err := e.Stream(context.Background(), func(l Line) {
		if l.Stream == StreamStderr {
			errLines = append(errLines, l.Text)
		}
	}, "sh", "-c", "echo e >&2")
	if err != nil {
		t.Fatal(err)
	}
	if res.Stderr != "e\n" {
		t.Errorf("captured stderr = %q, want %q", res.Stderr, "e\n")
	}
	if len(errLines) != 1 || errLines[0] != "e" {
		t.Errorf("stderr lines = %v, want [e]", errLines)
	}
}

func TestStreamNonZeroExit(t *testing.T) {
	e := quietExec(false)
	res, err := e.Stream(context.Background(), nil, "sh", "-c", "echo before; exit 2")
	code, ok := CommandExitCode(err)
	if !ok || code != 2 {
		t.Fatalf("want exit 2 command error, got code=%d ok=%v err=%v", code, ok, err)
	}
	if res.ExitCode != 2 {
		t.Errorf("res.ExitCode = %d, want 2", res.ExitCode)
	}
	var ce *wefterr.CmdError
	if !errors.As(err, &ce) {
		t.Errorf("want CmdError, got %T", err)
	}
	if res.Stdout != "before\n" {
		t.Errorf("captured stdout = %q, want %q", res.Stdout, "before\n")
	}
}

func TestStreamDryRunSkips(t *testing.T) {
	e := quietExec(true)
	res, err := e.Stream(context.Background(), func(l Line) {
		t.Errorf("dry-run should not stream, got %q", l.Text)
	}, "sh", "-c", "echo nope; exit 1")
	if err != nil {
		t.Fatalf("dry-run stream should not error, got %v", err)
	}
	if res.Stdout != "" {
		t.Errorf("dry-run should produce no output, got %q", res.Stdout)
	}
}

func TestStreamStartError(t *testing.T) {
	e := quietExec(false)
	_, err := e.Stream(context.Background(), nil, "weft-nonexistent-binary-xyz")
	if err == nil {
		t.Fatal("expected start error for missing binary")
	}
	// A start failure is surfaced raw, not shaped into a CmdError.
	if IsCommandError(err) {
		t.Errorf("start error should not be a CmdError, got %v", err)
	}
}

// A cancelled context is preserved as context.Canceled by cmdErr rather than
// being shaped into a CmdError, for both Run and Stream.
func TestContextCanceledPreserved(t *testing.T) {
	e := quietExec(false)

	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()
	_, runErr := e.Run(runCtx, "sh", "-c", "sleep 1")
	if !errors.Is(runErr, context.Canceled) {
		t.Errorf("Run: want context.Canceled, got %v", runErr)
	}
	if IsCommandError(runErr) {
		t.Errorf("Run: cancellation should not be a CmdError, got %v", runErr)
	}

	streamCtx, cancelStream := context.WithCancel(context.Background())
	cancelStream()
	_, streamErr := e.Stream(streamCtx, nil, "sh", "-c", "sleep 1")
	if !errors.Is(streamErr, context.Canceled) {
		t.Errorf("Stream: want context.Canceled, got %v", streamErr)
	}
	if IsCommandError(streamErr) {
		t.Errorf("Stream: cancellation should not be a CmdError, got %v", streamErr)
	}
}
