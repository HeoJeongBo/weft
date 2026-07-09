package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain lets TestMainEntry re-exec this instrumented test binary and run the
// real main(). When WEFT_BE_MAIN is set, we shape os.Args as if the weft binary
// had been invoked and call main() directly so os.Exit flushes coverage data.
func TestMain(m *testing.M) {
	if os.Getenv("WEFT_BE_MAIN") == "1" {
		os.Args = append([]string{"weft"}, strings.Split(os.Getenv("WEFT_MAIN_ARGS"), "\x00")...)
		main()
		return
	}
	os.Exit(m.Run())
}

func TestRun(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		wantOK bool // true => exit code 0
	}{
		{name: "version succeeds", args: []string{"version"}, wantOK: true},
		{name: "unknown flag errors", args: []string{"--totallybogus---flag"}, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := run(context.Background(), tt.args, io.Discard, io.Discard)
			if tt.wantOK && code != 0 {
				t.Fatalf("run(%v) = %d, want 0", tt.args, code)
			}
			if !tt.wantOK && code == 0 {
				t.Fatalf("run(%v) = 0, want non-zero", tt.args)
			}
		})
	}
}

// TestMainEntry drives the real main() in a subprocess so its single line is
// counted. It only runs under coverage, where the child inherits the
// instrumented binary and flushes profiles to GOCOVERDIR on os.Exit.
func TestMainEntry(t *testing.T) {
	if testing.CoverMode() == "" {
		t.Skip("requires -cover so the re-exec'd child is instrumented")
	}
	cmd := exec.Command(os.Args[0])
	// Inherit GOCOVERDIR (go test sets it under -cover) so the child's coverage
	// counters, flushed on os.Exit, merge into this package's report and main()
	// is counted. Fall back to a temp dir if it is somehow unset.
	env := append(os.Environ(), "WEFT_BE_MAIN=1", "WEFT_MAIN_ARGS=version")
	if os.Getenv("GOCOVERDIR") == "" {
		env = append(env, "GOCOVERDIR="+t.TempDir())
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("main() subprocess failed: %v\n%s", err, out)
	}
}
