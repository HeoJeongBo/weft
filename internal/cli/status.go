package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func newStatusCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status <name>",
		Short: "Show detailed status of a single session",
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
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), s)
			}
			printSessionDetail(cmd.OutOrStdout(), s, e.Project, colorEnabled(cmd))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}
