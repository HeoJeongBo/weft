package sysexec

import (
	"context"
	"strings"
	"sync"
)

// Call is a recorded invocation on a FakeRunner.
type Call struct {
	Kind string // "run", "mutate", or "stream"
	Name string
	Args []string
}

// Line returns the call as a single command line, for matching/assertions.
func (c Call) Line() string { return cmdline(c.Name, c.Args) }

// FakeRunner is a test double for Runner. It records every call and delegates to
// Handler for the result; a nil Handler returns empty success. Stream splits the
// handler's Stdout into lines and feeds them to the sink.
type FakeRunner struct {
	mu       sync.Mutex
	Calls    []Call
	Handler  func(c Call) (Result, error)
	LookFunc func(name string) (string, error)
}

func (f *FakeRunner) record(kind, name string, args []string) Call {
	c := Call{Kind: kind, Name: name, Args: append([]string(nil), args...)}
	f.mu.Lock()
	f.Calls = append(f.Calls, c)
	f.mu.Unlock()
	return c
}

func (f *FakeRunner) handle(c Call) (Result, error) {
	if f.Handler != nil {
		return f.Handler(c)
	}
	return Result{}, nil
}

// Run records and delegates.
func (f *FakeRunner) Run(_ context.Context, name string, args ...string) (Result, error) {
	return f.handle(f.record("run", name, args))
}

// Mutate records and delegates.
func (f *FakeRunner) Mutate(_ context.Context, name string, args ...string) (Result, error) {
	return f.handle(f.record("mutate", name, args))
}

// Stream records, delegates, then replays the resulting Stdout to sink line by line.
func (f *FakeRunner) Stream(_ context.Context, sink func(Line), name string, args ...string) (Result, error) {
	res, err := f.handle(f.record("stream", name, args))
	if sink != nil && res.Stdout != "" {
		for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
			sink(Line{Stream: StreamStdout, Text: ln})
		}
	}
	return res, err
}

// Look delegates to LookFunc, defaulting to "found".
func (f *FakeRunner) Look(name string) (string, error) {
	if f.LookFunc != nil {
		return f.LookFunc(name)
	}
	return "/usr/bin/" + name, nil
}

// LastCall returns the most recent recorded call, or the zero Call if none.
func (f *FakeRunner) LastCall() Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Calls) == 0 {
		return Call{}
	}
	return f.Calls[len(f.Calls)-1]
}
