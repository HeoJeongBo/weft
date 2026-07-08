package sysexec

import (
	"context"
	"errors"
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
