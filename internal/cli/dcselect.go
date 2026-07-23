package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/tmux"
)

func newDcSelectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "select <n>",
		Short: "Show the n-th devcontainer (used by the number key bindings)",
		Long: `Show the n-th devcontainer of the sidebar list in the weft/dc grid — the
command behind Ctrl+1…9 (and Option/prefix number keys). A stopped
devcontainer is brought up first. It only rearranges panes; run it from
inside the weft/dc session.`,
		Args: cobra.ExactArgs(1),
		RunE: runDcSelect,
	}
}

func runDcSelect(cmd *cobra.Command, args []string) error {
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return fmt.Errorf("select wants a number from the sidebar list, got %q", args[0])
	}
	r, verbosity, _ := dcRunner(cmd)
	cands, err := dcScan(cmd.Context(), r)
	if err != nil {
		return err
	}
	if n > len(cands) {
		return fmt.Errorf("no devcontainer #%d — the list has %d (run `weft dc`)", n, len(cands))
	}
	c := cands[n-1]
	if c.State != "running" {
		if err := dcUp(cmd, r, c, verbosity); err != nil {
			return err
		}
	}
	tm := tmux.New(r)
	paneID, err := dcShow(cmd.Context(), tm, c, dcLaunchArgs(c, false), true)
	if err != nil {
		return err
	}
	grid := dcTmuxSession + ":" + dcGridWindow
	_ = tm.SelectWindow(cmd.Context(), grid)
	if paneID != "" {
		_ = tm.SelectPane(cmd.Context(), paneID)
	}
	return nil
}
