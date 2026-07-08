package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/engine"
)

func newRmCmd() *cobra.Command {
	var force, deleteBranch, keepBranch, yes bool
	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Tear down a session (worktree, container, tmux window)",
		Long: `rm removes a session's tmux window, container, and worktree.

It refuses to run when the worktree has uncommitted or unpushed work unless you
pass --force. The branch is kept by default; use --delete-branch to remove it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			name := args[0]
			if !yes && !confirm(cmd, fmt.Sprintf("remove session %q?", name)) {
				fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
				return nil
			}
			spec := engine.RemoveSpec{
				Name:         name,
				Force:        force,
				DeleteBranch: deleteBranch,
				KeepBranch:   keepBranch,
			}
			if err := e.Remove(cmd.Context(), spec, progressSink(cmd)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s removed %q\n", colorize("✓", ansiGreen, colorEnabled(cmd)), name)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&force, "force", "f", false, "discard uncommitted/unpushed work and force removal")
	f.BoolVarP(&deleteBranch, "delete-branch", "b", false, "also delete the git branch")
	f.BoolVar(&keepBranch, "keep-branch", false, "keep the branch even if configured to delete it")
	f.BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}
