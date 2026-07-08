package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/engine"
	"github.com/HeoJeongBo/weft/internal/tmux"
	"github.com/HeoJeongBo/weft/internal/wefterr"
)

func newAttachCmd() *cobra.Command {
	var start bool
	cmd := &cobra.Command{
		Use:   "attach <name>",
		Short: "Attach to a session's tmux window",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			if start {
				if err := e.Start(cmd.Context(), args[0], false, progressSink(cmd)); err != nil {
					return err
				}
			}
			return attachToSession(cmd.Context(), e, args[0])
		},
	}
	cmd.Flags().BoolVar(&start, "start", false, "start the session first if it is stopped")
	return cmd
}

// attachToSession focuses and attaches to the session's tmux window. Inside tmux
// it switches the client (attach must not nest); outside tmux it hands the
// terminal to a blocking `tmux attach` and returns when the user detaches.
func attachToSession(ctx context.Context, e *engine.Engine, name string) error {
	s, ok, err := e.FindSession(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", wefterr.ErrSessionNotFound, name)
	}
	if s.Window == nil {
		return fmt.Errorf("session %s has no tmux window (try `weft attach %s --start`)", name, name)
	}

	target := e.Project.WindowTarget(name)
	_ = e.Tmux.SelectWindow(ctx, target)

	if tmux.InTmux() {
		return e.Tmux.SwitchClient(ctx, target)
	}

	c := exec.CommandContext(ctx, "tmux", tmux.AttachArgs(e.Project.TmuxSession)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
