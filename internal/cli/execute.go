package cli

import (
	"context"
	"io"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/fang"

	"github.com/HeoJeongBo/weft/internal/version"
)

// Execute runs the weft CLI through fang, which renders styled help, usage, and
// error output, plus an automatic --version. args are the CLI arguments (without
// the program name); stdout/stderr receive command output. It returns the
// resulting error; exit-code mapping stays with the caller (cli.ExitCode).
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return fang.Execute(ctx, root,
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
