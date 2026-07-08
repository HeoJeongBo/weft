package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/engine"
)

func newNewCmd() *cobra.Command {
	var (
		base, branch          string
		noDC, noClaude        bool
		attach, force, keepOF bool
	)
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a session: worktree + devcontainer + tmux window + claude",
		Long: `new weaves a full session in one motion:

  1. git worktree + branch    (weft/<name> from the base branch)
  2. devcontainer up          (labelled for reconciliation)
  3. post_create hooks
  4. tmux window running claude

If any step fails it rolls back the earlier ones (unless --keep-on-failure).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			spec := engine.NewSpec{
				Name:           args[0],
				BaseRef:        base,
				Branch:         branch,
				NoDevcontainer: noDC,
				NoClaude:       noClaude,
				Force:          force,
				KeepOnFailure:  keepOF,
			}
			s, err := e.New(cmd.Context(), spec, progressSink(cmd))
			if err != nil {
				return err
			}
			color := colorEnabled(cmd)
			fmt.Fprintf(cmd.OutOrStdout(), "%s session %q ready (%s)\n",
				colorize("✓", ansiGreen, color), s.Name, s.Branch)

			if attach {
				return attachToSession(cmd.Context(), e, s.Name)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "attach with: weft attach %s\n", s.Name)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&base, "base", "", "base ref to branch from (default: project base branch)")
	f.StringVar(&branch, "branch", "", "branch name (default: <prefix><name>)")
	f.BoolVar(&noDC, "no-devcontainer", false, "skip bringing up a devcontainer")
	f.BoolVar(&noClaude, "no-claude", false, "open a shell instead of launching claude")
	f.BoolVarP(&attach, "attach", "a", false, "attach to the session after creating it")
	f.BoolVar(&force, "force", false, "reuse an existing branch")
	f.BoolVar(&keepOF, "keep-on-failure", false, "do not roll back if a step fails")
	return cmd
}
