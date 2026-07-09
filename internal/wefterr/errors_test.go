package wefterr

import (
	"errors"
	"fmt"
	"testing"
)

func TestCmdErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *CmdError
		want string
	}{
		{
			name: "with stderr",
			err:  &CmdError{Cmd: "git", Args: []string{"status"}, ExitCode: 1, Stderr: "boom"},
			want: "git status: exit 1: boom",
		},
		{
			name: "without stderr",
			err:  &CmdError{Cmd: "git", Args: []string{"status"}, ExitCode: 2, Stderr: ""},
			want: "git status: exit 2",
		},
		{
			name: "whitespace-only stderr",
			err:  &CmdError{Cmd: "git", Args: []string{"status"}, ExitCode: 2, Stderr: "  \n  "},
			want: "git status: exit 2",
		},
		{
			name: "multiline stderr keeps first line",
			err:  &CmdError{Cmd: "docker", Args: []string{"ps", "-a"}, ExitCode: 3, Stderr: "line1\nline2\nline3"},
			want: "docker ps -a: exit 3: line1",
		},
		{
			name: "no args",
			err:  &CmdError{Cmd: "tmux", Args: nil, ExitCode: 4, Stderr: "err"},
			want: "tmux : exit 4: err",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWithExitCode(t *testing.T) {
	if got := WithExitCode(nil, 5); got != nil {
		t.Errorf("WithExitCode(nil, 5) = %v, want nil", got)
	}

	base := errors.New("base failure")
	wrapped := WithExitCode(base, 7)
	if wrapped == nil {
		t.Fatal("WithExitCode(non-nil) returned nil")
	}
	// coded.Error delegates to the wrapped error.
	if wrapped.Error() != "base failure" {
		t.Errorf("wrapped.Error() = %q, want %q", wrapped.Error(), "base failure")
	}
	// coded.Unwrap is reachable via errors.Is.
	if !errors.Is(wrapped, base) {
		t.Error("errors.Is(wrapped, base) = false, want true")
	}
	if ExitCode(wrapped) != 7 {
		t.Errorf("ExitCode(wrapped) = %d, want 7", ExitCode(wrapped))
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, CodeOK},
		{"coded", WithExitCode(errors.New("x"), 42), 42},
		{"coded takes precedence over sentinel", WithExitCode(ErrDirtyWorktree, 9), 9},
		{"dependency missing", ErrDependencyMissing, CodeDependency},
		{"dependency wrapped", fmt.Errorf("preflight: %w", ErrDependencyMissing), CodeDependency},
		{"dirty worktree", ErrDirtyWorktree, CodeDataLoss},
		{"other", errors.New("boom"), CodeGeneric},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no newline", "single line", "single line"},
		{"with newline", "first\nsecond", "first"},
		{"leading newline", "\nsecond", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine(tt.in); got != tt.want {
				t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
