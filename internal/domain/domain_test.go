package domain

import "testing"

func TestValidName(t *testing.T) {
	valid := []string{"feat-auth", "fix_webhook", "v1.2", "ABC123"}
	invalid := []string{"", "has space", "weft/x", "a/b", "emoji😀"}
	for _, s := range valid {
		if !ValidName(s) {
			t.Errorf("ValidName(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if ValidName(s) {
			t.Errorf("ValidName(%q) = true, want false", s)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My App":       "my-app",
		"acme_API!!":   "acme-api",
		"  weird  ":    "weird",
		"Foo.Bar-Baz":  "foo-bar-baz",
		"already-slug": "already-slug",
		"UPPER":        "upper",
		"a--b":         "a-b",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveStatus(t *testing.T) {
	wt := &Worktree{Path: "/wt"}
	running := &Container{State: "running"}
	created := &Container{State: "created"}
	exited := &Container{State: "exited"}
	win := func() *Window { return &Window{ID: "@1", Name: "x", PaneCommand: "claude"} }

	tests := []struct {
		name   string
		s      Session
		dc     bool // devcontainer expected
		want   SessionStatus
		claude ClaudeState
	}{
		// Devcontainer-backed sessions: container state drives status.
		{"ready", Session{Worktree: wt, Container: running, Window: win()}, true, StatusReady, ClaudeRunning},
		{"starting", Session{Worktree: wt, Container: created, Window: win()}, true, StatusStarting, ClaudeRunning},
		{"stopped-exited", Session{Worktree: wt, Container: exited, Window: win()}, true, StatusStopped, ClaudeRunning},
		{"stopped-nocontainer", Session{Worktree: wt, Window: win()}, true, StatusStopped, ClaudeRunning},
		{"partial", Session{Worktree: wt}, true, StatusPartial, ClaudeNone},
		{"orphaned", Session{Container: running}, true, StatusOrphaned, ClaudeNone},
		{"dead-window", Session{Worktree: wt, Container: running, Window: &Window{PaneDead: true, PaneCommand: "claude"}}, true, StatusStopped, ClaudeDead},
		{"idle-claude", Session{Worktree: wt, Container: running, Window: &Window{PaneCommand: "bash"}}, true, StatusReady, ClaudeIdle},
		// tmux-only sessions (no devcontainer expected): a live window is Ready.
		{"nodc-ready", Session{Worktree: wt, Window: win()}, false, StatusReady, ClaudeRunning},
		{"nodc-stopped", Session{Worktree: wt}, false, StatusStopped, ClaudeNone},
		{"nodc-orphan", Session{Container: running}, false, StatusOrphaned, ClaudeNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.DeriveStatus(tt.dc, "claude", "node")
			if tt.s.Status != tt.want {
				t.Errorf("Status = %q, want %q", tt.s.Status, tt.want)
			}
			if tt.s.Claude != tt.claude {
				t.Errorf("Claude = %q, want %q", tt.s.Claude, tt.claude)
			}
		})
	}
}
