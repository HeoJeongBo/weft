package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <name> [--] <cmd>...",
		Short: "Run a command inside a session's container",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			name := args[0]
			cmdArgs := args[1:]
			if len(cmdArgs) > 0 && cmdArgs[0] == "--" {
				cmdArgs = cmdArgs[1:]
			}
			if len(cmdArgs) == 0 {
				return fmt.Errorf("no command given")
			}
			res, execErr := e.Exec(cmd.Context(), name, cmdArgs...)
			if res.Stdout != "" {
				fmt.Fprint(cmd.OutOrStdout(), res.Stdout)
			}
			if res.Stderr != "" {
				fmt.Fprint(cmd.ErrOrStderr(), res.Stderr)
			}
			return execErr
		},
	}
	// Treat flags after the session name as part of the command.
	cmd.Flags().SetInterspersed(false)
	return cmd
}
