package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/engine"
	"github.com/HeoJeongBo/weft/internal/logx"
	"github.com/HeoJeongBo/weft/internal/sysexec"
)

// runnerFromCmd builds a sysexec.Runner honoring --verbose and --dry-run.
func runnerFromCmd(cmd *cobra.Command) sysexec.Runner {
	verbosity, _ := cmd.Flags().GetCount("verbose")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	log := logx.New(os.Stderr, verbosity, false)
	return sysexec.New(dryRun, log)
}

// openEngine resolves the current repository into an Engine.
func openEngine(cmd *cobra.Command) (*engine.Engine, error) {
	verbosity, _ := cmd.Flags().GetCount("verbose")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	cfgPath, _ := cmd.Flags().GetString("config")
	log := logx.New(os.Stderr, verbosity, false)
	r := sysexec.New(dryRun, log)

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return engine.Open(cmd.Context(), r, log, cwd, cfgPath)
}

// progressSink returns an engine.Sink that prints step headers to stderr, and
// streamed log lines only when -v is set. Terminal EventDone/EventError are left
// to the caller.
func progressSink(cmd *cobra.Command) engine.Sink {
	verbosity, _ := cmd.Flags().GetCount("verbose")
	color := colorEnabled(cmd)
	out := cmd.ErrOrStderr()
	return func(ev engine.Event) {
		switch ev.Kind {
		case engine.EventStep:
			fmt.Fprintf(out, "%s %s\n", colorize("▶", ansiCyan, color), ev.Step)
		case engine.EventLog:
			if verbosity > 0 {
				fmt.Fprintf(out, "   %s\n", colorize(ev.Text, ansiDim, color))
			}
		}
	}
}
