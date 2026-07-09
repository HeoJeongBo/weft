package cli

import (
	"context"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/fang"

	"github.com/HeoJeongBo/weft/internal/version"
)

// Execute runs the weft CLI through fang, which renders styled help, usage, and
// error output, plus an automatic --version. It returns the resulting error;
// exit-code mapping stays with the caller (cli.ExitCode).
func Execute(ctx context.Context) error {
	return fang.Execute(ctx, NewRootCmd(),
		fang.WithVersion(version.Version),
		fang.WithCommit(version.Commit),
		fang.WithColorSchemeFunc(weftColorScheme),
	)
}

// weftColorScheme starts from fang's default scheme and swaps the accent to
// weft's cyan, matching the TUI.
func weftColorScheme(c lipgloss.LightDarkFunc) fang.ColorScheme {
	s := fang.DefaultColorScheme(c)
	accent := lipgloss.Color("6") // cyan
	s.Title = accent
	s.Program = accent
	s.Command = accent
	return s
}
