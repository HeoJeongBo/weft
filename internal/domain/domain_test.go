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
		want   SessionStatus
		claude ClaudeState
	}{
		{"ready", Session{Worktree: wt, Container: running, Window: win()}, StatusReady, ClaudeRunning},
		{"starting", Session{Worktree: wt, Container: created, Window: win()}, StatusStarting, ClaudeRunning},
		{"stopped-exited", Session{Worktree: wt, Container: exited, Window: win()}, StatusStopped, ClaudeRunning},
		{"stopped-nocontainer", Session{Worktree: wt, Window: win()}, StatusStopped, ClaudeRunning},
		{"partial", Session{Worktree: wt}, StatusPartial, ClaudeNone},
		{"orphaned", Session{Container: running}, StatusOrphaned, ClaudeNone},
		{"dead-window", Session{Worktree: wt, Container: running, Window: &Window{PaneDead: true, PaneCommand: "claude"}}, StatusStopped, ClaudeDead},
		{"idle-claude", Session{Worktree: wt, Container: running, Window: &Window{PaneCommand: "bash"}}, StatusReady, ClaudeIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.DeriveStatus("claude", "node")
			if tt.s.Status != tt.want {
				t.Errorf("Status = %q, want %q", tt.s.Status, tt.want)
			}
			if tt.s.Claude != tt.claude {
				t.Errorf("Claude = %q, want %q", tt.s.Claude, tt.claude)
			}
		})
	}
}
