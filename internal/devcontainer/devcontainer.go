// Package devcontainer wraps the official @devcontainers/cli (`devcontainer up`
// and `devcontainer exec`).
package devcontainer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

// UpOpts configures `devcontainer up`.
type UpOpts struct {
	WorkspaceFolder string
	ConfigPath      string   // --config; optional
	IDLabels        []string // --id-label key=value (repeatable)
	RemoveExisting  bool     // --remove-existing-container
	ExtraArgs       []string // passthrough flags
}

// ExecOpts identifies the target container for `devcontainer exec`.
type ExecOpts struct {
	WorkspaceFolder string
	ConfigPath      string
	IDLabels        []string
}

// UpResult is the JSON object `devcontainer up` prints on its final stdout line.
type UpResult struct {
	Outcome               string `json:"outcome"`
	ContainerID           string `json:"containerId"`
	RemoteUser            string `json:"remoteUser"`
	RemoteWorkspaceFolder string `json:"remoteWorkspaceFolder"`
	Message               string `json:"message"`
	Description           string `json:"description"`
}

// Devcontainer is the subset of the CLI weft depends on.
type Devcontainer interface {
	Up(ctx context.Context, sink func(sysexec.Line), opts UpOpts) (UpResult, error)
	Exec(ctx context.Context, opts ExecOpts, cmd ...string) (sysexec.Result, error)
	Available(ctx context.Context) (string, error)
}

// Exec is the real Devcontainer backed by a sysexec.Runner.
type Exec struct {
	r sysexec.Runner
}

// New returns a Devcontainer backed by r.
func New(r sysexec.Runner) *Exec { return &Exec{r: r} }

// Up brings the container up, streaming build/output lines to sink (which may be
// nil), and returns the parsed result.
func (e *Exec) Up(ctx context.Context, sink func(sysexec.Line), opts UpOpts) (UpResult, error) {
	res, runErr := e.r.Stream(ctx, sink, "devcontainer", upArgs(opts)...)

	up, parseErr := parseUpResult(res.Stdout)
	if runErr != nil {
		if parseErr == nil && up.Description != "" {
			return up, fmt.Errorf("devcontainer up failed: %s", up.Description)
		}
		return up, runErr
	}
	if parseErr != nil {
		return up, fmt.Errorf("devcontainer up: could not parse result: %w", parseErr)
	}
	if up.Outcome != "success" {
		return up, fmt.Errorf("devcontainer up failed: %s", firstNonEmpty(up.Description, up.Message, up.Outcome))
	}
	return up, nil
}

// Exec runs a command in the container and captures its output.
func (e *Exec) Exec(ctx context.Context, opts ExecOpts, cmd ...string) (sysexec.Result, error) {
	argv := ExecArgs(opts, cmd...)
	return e.r.Run(ctx, argv[0], argv[1:]...)
}

// Available returns the installed devcontainer CLI version.
func (e *Exec) Available(ctx context.Context) (string, error) {
	res, err := e.r.Run(ctx, "devcontainer", "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ExecArgs builds the full argv (including the "devcontainer" binary) to run cmd
// inside the target container. Used both by Exec and to construct the tmux
// window's foreground command.
func ExecArgs(o ExecOpts, cmd ...string) []string {
	args := []string{"devcontainer", "exec", "--workspace-folder", o.WorkspaceFolder}
	if o.ConfigPath != "" {
		args = append(args, "--config", o.ConfigPath)
	}
	for _, l := range o.IDLabels {
		args = append(args, "--id-label", l)
	}
	return append(args, cmd...)
}

func upArgs(o UpOpts) []string {
	args := []string{"up", "--workspace-folder", o.WorkspaceFolder}
	if o.ConfigPath != "" {
		args = append(args, "--config", o.ConfigPath)
	}
	for _, l := range o.IDLabels {
		args = append(args, "--id-label", l)
	}
	if o.RemoveExisting {
		args = append(args, "--remove-existing-container")
	}
	return append(args, o.ExtraArgs...)
}

// parseUpResult finds the last stdout line that is a devcontainer result object.
func parseUpResult(stdout string) (UpResult, error) {
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var u UpResult
		if err := json.Unmarshal([]byte(line), &u); err == nil && u.Outcome != "" {
			return u, nil
		}
	}
	return UpResult{}, errors.New("no result object on stdout")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
