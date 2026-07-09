package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigHome(t *testing.T) {
	t.Run("xdg set", func(t *testing.T) {
		t.Setenv("HOME", "/home/user")
		t.Setenv("XDG_CONFIG_HOME", "/custom/config")
		if got := ConfigHome(); got != "/custom/config" {
			t.Errorf("ConfigHome() = %q, want /custom/config", got)
		}
	})
	t.Run("xdg unset", func(t *testing.T) {
		t.Setenv("HOME", "/home/user")
		t.Setenv("XDG_CONFIG_HOME", "")
		want := filepath.Join("/home/user", ".config")
		if got := ConfigHome(); got != want {
			t.Errorf("ConfigHome() = %q, want %q", got, want)
		}
	})
}

func TestStateHome(t *testing.T) {
	t.Run("xdg set", func(t *testing.T) {
		t.Setenv("HOME", "/home/user")
		t.Setenv("XDG_STATE_HOME", "/custom/state")
		if got := StateHome(); got != "/custom/state" {
			t.Errorf("StateHome() = %q, want /custom/state", got)
		}
	})
	t.Run("xdg unset", func(t *testing.T) {
		t.Setenv("HOME", "/home/user")
		t.Setenv("XDG_STATE_HOME", "")
		want := filepath.Join("/home/user", ".local", "state")
		if got := StateHome(); got != want {
			t.Errorf("StateHome() = %q, want %q", got, want)
		}
	})
}

func TestUserConfig(t *testing.T) {
	t.Setenv("HOME", "/home/user")
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	want := filepath.Join("/custom/config", "weft", "config.yaml")
	if got := UserConfig(); got != want {
		t.Errorf("UserConfig() = %q, want %q", got, want)
	}
}

func TestStateDir(t *testing.T) {
	t.Setenv("HOME", "/home/user")
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	want := filepath.Join("/custom/state", "weft", "proj-slug")
	if got := StateDir("proj-slug"); got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestExpandTilde(t *testing.T) {
	t.Setenv("HOME", "/home/user")
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare tilde", "~", "/home/user"},
		{"tilde slash", "~/sub/dir", filepath.Join("/home/user", "sub/dir")},
		{"plain", "plain/path", "plain/path"},
		{"empty", "", ""},
		{"tilde no slash", "~foo", "~foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpandTilde(tt.in); got != tt.want {
				t.Errorf("ExpandTilde(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"existing file", f, true},
		{"existing dir", dir, true},
		{"missing", filepath.Join(dir, "nope.txt"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Exists(tt.path); got != tt.want {
				t.Errorf("Exists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestBaseDir(t *testing.T) {
	t.Setenv("HOME", "/home/user")
	t.Run("env set", func(t *testing.T) {
		t.Setenv("WEFT_TEST_XDG", "/from/env")
		if got := baseDir("WEFT_TEST_XDG", "fallback"); got != "/from/env" {
			t.Errorf("baseDir() = %q, want /from/env", got)
		}
	})
	t.Run("env empty", func(t *testing.T) {
		t.Setenv("WEFT_TEST_XDG", "")
		want := filepath.Join("/home/user", "fallback")
		if got := baseDir("WEFT_TEST_XDG", "fallback"); got != want {
			t.Errorf("baseDir() = %q, want %q", got, want)
		}
	})
}

func TestHome(t *testing.T) {
	t.Run("home set", func(t *testing.T) {
		t.Setenv("HOME", "/home/user")
		if got := home(); got != "/home/user" {
			t.Errorf("home() = %q, want /home/user", got)
		}
	})
	t.Run("home unset falls back to dot", func(t *testing.T) {
		// On this Unix host os.UserHomeDir errors when HOME is empty.
		t.Setenv("HOME", "")
		if got := home(); got != "." {
			t.Errorf("home() = %q, want .", got)
		}
	})
}
