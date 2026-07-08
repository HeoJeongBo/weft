// Package paths resolves XDG base directories and weft's locations within them.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// ConfigHome returns $XDG_CONFIG_HOME or ~/.config.
func ConfigHome() string { return baseDir("XDG_CONFIG_HOME", ".config") }

// StateHome returns $XDG_STATE_HOME or ~/.local/state.
func StateHome() string { return baseDir("XDG_STATE_HOME", filepath.Join(".local", "state")) }

// UserConfig returns the path to the user-level weft config file.
func UserConfig() string { return filepath.Join(ConfigHome(), "weft", "config.yaml") }

// StateDir returns the per-project state directory (non-authoritative cache).
func StateDir(slug string) string { return filepath.Join(StateHome(), "weft", slug) }

// ExpandTilde expands a leading ~ or ~/ to the user's home directory.
func ExpandTilde(p string) string {
	switch {
	case p == "~":
		return home()
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home(), p[2:])
	default:
		return p
	}
}

// Exists reports whether a path exists.
func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func baseDir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return filepath.Join(home(), fallback)
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}
