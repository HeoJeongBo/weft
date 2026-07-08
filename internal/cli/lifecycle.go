package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func newStartCmd() *cobra.Command {
	var noClaude bool
	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Resume a stopped session (bring its container back up)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			if err := e.Start(cmd.Context(), args[0], noClaude, progressSink(cmd)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s started %q\n", colorize("✓", ansiGreen, colorEnabled(cmd)), args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&noClaude, "no-claude", false, "open a shell instead of launching claude")
	return cmd
}

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Pause a session (stop its container, keep the worktree)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			if err := e.Stop(cmd.Context(), args[0], progressSink(cmd)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s stopped %q\n", colorize("✓", ansiGreen, colorEnabled(cmd)), args[0])
			return nil
		},
	}
	return cmd
}

func newCdCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cd <name>",
		Short: "Print a session's worktree path (use: cd \"$(weft cd <name>)\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			s, ok, err := e.FindSession(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: %s", wefterr.ErrSessionNotFound, args[0])
			}
			if s.Worktree == nil {
				return fmt.Errorf("session %s has no worktree", args[0])
			}
			fmt.Fprintln(cmd.OutOrStdout(), s.Worktree.Path)
			return nil
		},
	}
	return cmd
}
