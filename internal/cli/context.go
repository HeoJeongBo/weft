package cli

import (
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
