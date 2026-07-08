package devcontainer

import (
	"context"
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
