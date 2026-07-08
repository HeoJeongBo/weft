// Package cli assembles weft's cobra command tree.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/version"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// NewRootCmd builds the root command with all subcommands attached.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "weft",
		Short: "Orchestrate parallel Claude Code sessions across git worktrees + devcontainers",
		Long: `weft weaves git worktrees, devcontainers, tmux, and Claude Code into one
motion, and gives you a dashboard over every parallel session.

Run "weft" with no arguments to open the dashboard, or use the subcommands to
script the same actions.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// TODO(m5): launch the TUI dashboard. For now, show help.
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")

	pf := root.PersistentFlags()
	pf.CountP("verbose", "v", "increase log verbosity (-v, -vv)")
	pf.Bool("dry-run", false, "print actions instead of executing them")
	pf.String("config", "", "path to weft.yaml (overrides discovery)")
	pf.Bool("no-color", false, "disable colored output")

	root.AddCommand(
		newVersionCmd(),
		newDoctorCmd(),
		newInitCmd(),
		newLsCmd(),
		newStatusCmd(),
	)
	return root
}

// ExitCode maps an error returned from command execution to a process exit code.
func ExitCode(err error) int {
	return wefterr.ExitCode(err)
}
