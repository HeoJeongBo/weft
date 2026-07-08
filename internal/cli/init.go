package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a weft.yaml in the current repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEngine(cmd)
			if err != nil {
				return err
			}
			target := filepath.Join(e.Project.Root, "weft.yaml")
			if _, err := os.Stat(target); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", target)
			}

			dcPath, dcEnabled := detectDevcontainer(e.Project.Root)
			content := renderInitConfig(e.Project.Name, e.Project.DefaultBranch, dcPath, dcEnabled)
			if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", target)
			if !dcEnabled {
				fmt.Fprintln(cmd.OutOrStdout(), "note: no devcontainer.json detected — set devcontainer.config or disable it")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing weft.yaml")
	return cmd
}

// detectDevcontainer looks for a devcontainer config and returns its repo-relative
// path plus whether one was found.
func detectDevcontainer(root string) (string, bool) {
	candidates := []string{
		".devcontainer/devcontainer.json",
		".devcontainer.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(root, c)); err == nil {
			return c, true
		}
	}
	return ".devcontainer/devcontainer.json", false
}

func renderInitConfig(name, baseBranch, dcPath string, dcEnabled bool) string {
	return fmt.Sprintf(`# weft.yaml — see weft.yaml.example for all options.
version: 1

project:
  name: %s
  base_branch: %s

branch:
  prefix: "weft/"

worktree:
  root: "~/.weft/worktrees/{project}"

devcontainer:
  enabled: %t
  config: "%s"

tmux:
  session: "weft/{project}"
  window: "{name}"

claude:
  command: "claude"
  exec_in_container: true

cleanup:
  require_clean: true
`, name, baseBranch, dcEnabled, dcPath)
}
