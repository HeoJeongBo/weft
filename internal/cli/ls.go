package cli

import (
	"github.com/spf13/cobra"
)

func newLsCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List sessions with live, reconciled status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLs(cmd, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

// runLs reconciles and prints the session list. Shared by `weft ls` and the
// no-TTY fallback of the root command.
func runLs(cmd *cobra.Command, jsonOut bool) error {
	e, err := openEngine(cmd)
	if err != nil {
		return err
	}
	sessions, err := e.Reconcile(cmd.Context())
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), sessions)
	}
	printSessionsTable(cmd.OutOrStdout(), sessions, colorEnabled(cmd))
	return nil
}
