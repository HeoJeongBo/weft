package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRepairCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "repair",
		Aliases: []string{"prune"},
		Short:   "Reconcile and clean up orphaned worktrees, containers, and windows",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			if !yes && !confirm(cmd, "clean up orphaned resources?") {
				fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
				return nil
			}
			rep, err := e.Repair(cmd.Context(), progressSink(cmd))
			if err != nil {
				return err
			}
			pruned := ""
			if rep.PrunedWorktrees {
				pruned = ", pruned worktree metadata"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s repaired: %d orphan container(s), %d orphan window(s)%s\n",
				colorize("✓", ansiGreen, colorEnabled(cmd)), rep.OrphanContainers, rep.OrphanWindows, pruned)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}
