package devcontainer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

func TestUpSuccessParsesTrailingJSON(t *testing.T) {
	// Realistic output: build logs first, result object on the final line.
	stdout := strings.Join([]string{
		`[1 ms] @devcontainers/cli 0.84.1`,
		`[+] Building 2.3s`,
		`{"outcome":"success","containerId":"abc123","remoteUser":"vscode","remoteWorkspaceFolder":"/workspaces/app"}`,
	}, "\n")

	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: stdout}, nil
	}}
	dc := New(f)

	var lines []string
	up, err := dc.Up(context.Background(), func(l sysexec.Line) { lines = append(lines, l.Text) }, UpOpts{
		WorkspaceFolder: "/wt",
		IDLabels:        []string{"weft.session=app/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.ContainerID != "abc123" || up.RemoteUser != "vscode" || up.RemoteWorkspaceFolder != "/workspaces/app" {
		t.Errorf("bad result: %+v", up)
	}
	if len(lines) != 3 {
		t.Errorf("want 3 streamed lines, got %d", len(lines))
	}
}

func TestUpErrorOutcome(t *testing.T) {
	stdout := `{"outcome":"error","message":"boom","description":"image build failed"}`
	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: stdout}, nil
	}}
	dc := New(f)

	_, err := dc.Up(context.Background(), nil, UpOpts{WorkspaceFolder: "/wt"})
	if err == nil || !strings.Contains(err.Error(), "image build failed") {
		t.Fatalf("want error with description, got %v", err)
	}
}

func TestUpArgs(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: `{"outcome":"success","containerId":"x"}`}, nil
	}}
	dc := New(f)
	_, err := dc.Up(context.Background(), nil, UpOpts{
		WorkspaceFolder: "/wt",
		ConfigPath:      ".devcontainer/devcontainer.json",
		IDLabels:        []string{"weft.session=app/x", "weft.project=app"},
		RemoveExisting:  true,
		ExtraArgs:       []string{"--build-no-cache"},
	})
	if err != nil {
		t.Fatal(err)
	}
	line := f.LastCall().Line()
	for _, want := range []string{
		"up", "--workspace-folder /wt", "--config .devcontainer/devcontainer.json",
		"--id-label weft.session=app/x", "--id-label weft.project=app",
		"--remove-existing-container", "--build-no-cache",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("argv %q missing %q", line, want)
		}
	}
	if f.LastCall().Kind != "stream" {
		t.Errorf("up should Stream, got %q", f.LastCall().Kind)
	}
}

func TestExecArgs(t *testing.T) {
	argv := ExecArgs(ExecOpts{
		WorkspaceFolder: "/wt",
		IDLabels:        []string{"weft.session=app/x"},
	}, "claude", "--dangerously-skip-permissions")
	got := strings.Join(argv, " ")
	want := "devcontainer exec --workspace-folder /wt --id-label weft.session=app/x claude --dangerously-skip-permissions"
	if got != want {
		t.Errorf("ExecArgs =\n  %q\nwant\n  %q", got, want)
	}
}

func TestExecArgsWithConfig(t *testing.T) {
	argv := ExecArgs(ExecOpts{
		WorkspaceFolder: "/wt",
		ConfigPath:      ".devcontainer/devcontainer.json",
		IDLabels:        []string{"weft.session=app/x"},
	}, "bash")
	got := strings.Join(argv, " ")
	want := "devcontainer exec --workspace-folder /wt --config .devcontainer/devcontainer.json --id-label weft.session=app/x bash"
	if got != want {
		t.Errorf("ExecArgs =\n  %q\nwant\n  %q", got, want)
	}
}

// TestExec exercises Exec: it should Run the ExecArgs argv over the fake and
// return the Result unchanged.
func TestExec(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "ok", ExitCode: 0}, nil
	}}
	opts := ExecOpts{
		WorkspaceFolder: "/wt",
		ConfigPath:      "cfg.json",
		IDLabels:        []string{"weft.session=app/x"},
	}
	res, err := New(f).Exec(context.Background(), opts, "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "ok" {
		t.Errorf("stdout = %q, want ok", res.Stdout)
	}
	if wantLine := strings.Join(ExecArgs(opts, "echo", "hi"), " "); f.LastCall().Line() != wantLine {
		t.Errorf("argv = %q, want %q", f.LastCall().Line(), wantLine)
	}
	if f.LastCall().Kind != "run" {
		t.Errorf("exec should Run, got %q", f.LastCall().Kind)
	}
}

func TestAvailable(t *testing.T) {
	t.Run("success trims version", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
			return sysexec.Result{Stdout: "  0.84.1\n"}, nil
		}}
		v, err := New(f).Available(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if v != "0.84.1" {
			t.Errorf("version = %q, want 0.84.1", v)
		}
		if !strings.Contains(f.LastCall().Line(), "--version") {
			t.Errorf("expected --version, got %q", f.LastCall().Line())
		}
	})
	t.Run("error returns empty", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
			return sysexec.Result{Stdout: "0.84.1"}, errors.New("not installed")
		}}
		v, err := New(f).Available(context.Background())
		if err == nil {
			t.Fatal("want error")
		}
		if v != "" {
			t.Errorf("version = %q, want empty", v)
		}
	})
}

// TestUpBranches covers every path in Up beyond the success/error-outcome cases
// already tested above: dry-run, parse failures, and the runErr combinations.
func TestUpBranches(t *testing.T) {
	tests := []struct {
		name        string
		stdout      string
		runErr      error
		dryRun      bool
		wantErr     string // substring; "" means expect no error
		wantOutcome string
		wantCID     string
	}{
		{
			name:        "dry-run short-circuits",
			dryRun:      true,
			wantOutcome: "success",
			wantCID:     "dry-run",
		},
		{
			name:    "parse error when no json object line",
			stdout:  "just build logs\nno result here",
			wantErr: "could not parse result",
		},
		{
			name:    "parse error skips malformed and outcome-less json",
			stdout:  "[log] building\n{bad json\n{\"foo\":1}",
			wantErr: "could not parse result",
		},
		{
			name:        "runErr with parsed description wraps description",
			stdout:      `{"outcome":"error","description":"disk full"}`,
			runErr:      errors.New("exit status 1"),
			wantErr:     "disk full",
			wantOutcome: "error",
		},
		{
			name:    "runErr without parseable description returns runErr",
			stdout:  "garbage, not json",
			runErr:  errors.New("boom exit 1"),
			wantErr: "boom exit 1",
		},
		{
			name:        "runErr parsed but empty description returns runErr",
			stdout:      `{"outcome":"error"}`,
			runErr:      errors.New("boom exit 2"),
			wantErr:     "boom exit 2",
			wantOutcome: "error",
		},
		{
			name:        "outcome not success uses message",
			stdout:      `{"outcome":"error","message":"boom"}`,
			wantErr:     "boom",
			wantOutcome: "error",
		},
		{
			name:        "outcome not success falls back to outcome",
			stdout:      `{"outcome":"failure"}`,
			wantErr:     "failure",
			wantOutcome: "failure",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &sysexec.FakeRunner{
				DryRunMode: tt.dryRun,
				Handler: func(sysexec.Call) (sysexec.Result, error) {
					return sysexec.Result{Stdout: tt.stdout}, tt.runErr
				},
			}
			up, err := New(f).Up(context.Background(), nil, UpOpts{WorkspaceFolder: "/wt"})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if tt.wantOutcome != "" && up.Outcome != tt.wantOutcome {
				t.Errorf("outcome = %q, want %q", up.Outcome, tt.wantOutcome)
			}
			if tt.wantCID != "" && up.ContainerID != tt.wantCID {
				t.Errorf("containerID = %q, want %q", up.ContainerID, tt.wantCID)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"first non-empty", []string{"a", "b"}, "a"},
		{"skips leading empty", []string{"", "b"}, "b"},
		{"all empty", []string{"", ""}, ""},
		{"no args", nil, ""},
	}
	for _, tt := range tests {
		if got := firstNonEmpty(tt.in...); got != tt.want {
			t.Errorf("%s: firstNonEmpty(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
