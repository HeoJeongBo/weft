package domain

import "testing"

func TestSessionKey(t *testing.T) {
	cases := []struct {
		project, name, want string
	}{
		{"app", "feat", "app/feat"},
		{"", "feat", "/feat"},
		{"app", "", "app/"},
		{"", "", "/"},
	}
	for _, tc := range cases {
		if got := SessionKey(tc.project, tc.name); got != tc.want {
			t.Errorf("SessionKey(%q, %q) = %q, want %q", tc.project, tc.name, got, tc.want)
		}
	}
}

func TestSplitSessionKey(t *testing.T) {
	tests := []struct {
		key         string
		wantProject string
		wantName    string
		wantOK      bool
	}{
		{"a/b", "a", "b", true},
		{"noslash", "noslash", "", false},
		{"", "", "", false},
		{"a/b/c", "a", "b/c", true},
		{"/x", "", "x", true},
		{"app/", "app", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			project, name, ok := SplitSessionKey(tt.key)
			if project != tt.wantProject || name != tt.wantName || ok != tt.wantOK {
				t.Errorf("SplitSessionKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.key, project, name, ok, tt.wantProject, tt.wantName, tt.wantOK)
			}
		})
	}
}

func TestWindowTarget(t *testing.T) {
	cases := []struct {
		session, name, want string
	}{
		{"weft/app", "feat", "weft/app:feat"},
		{"", "feat", ":feat"},
		{"weft/app", "", "weft/app:"},
	}
	for _, tc := range cases {
		p := Project{TmuxSession: tc.session}
		if got := p.WindowTarget(tc.name); got != tc.want {
			t.Errorf("WindowTarget(%q) with session %q = %q, want %q", tc.name, tc.session, got, tc.want)
		}
	}
}

func TestSessionKeyMethod(t *testing.T) {
	s := &Session{Project: "app", Name: "feat"}
	if got := s.Key(); got != "app/feat" {
		t.Errorf("Key() = %q, want app/feat", got)
	}
}

func TestContainerRunning(t *testing.T) {
	tests := []struct {
		name string
		c    *Container
		want bool
	}{
		{"nil-receiver", nil, false},
		{"running", &Container{State: "running"}, true},
		{"exited", &Container{State: "exited"}, false},
		{"created", &Container{State: "created"}, false},
		{"empty-state", &Container{State: ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Running(); got != tt.want {
				t.Errorf("Running() = %v, want %v", got, tt.want)
			}
		})
	}
}
