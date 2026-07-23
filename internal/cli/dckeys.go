package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ghosttyDigits are the key names ghostty uses for the digit row.
var ghosttyDigits = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

func newDcKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keys",
		Short: "Print the terminal keybind snippet that enables Ctrl+1…9 switching",
		Long: `Print keybind lines for your terminal config that make Ctrl+1…9 send the
sequences weft binds in tmux — one chord to jump to the n-th devcontainer.
weft never edits your terminal config; paste these yourself.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "# ghostty — paste into your ghostty config, then reload it (cmd+shift+,)")
			for i, name := range ghosttyDigits {
				fmt.Fprintf(out, "keybind = ctrl+%s=text:\\x1b[weft%d~\n", name, i+1)
			}
			fmt.Fprintln(out, "\n# then, inside weft dc, Ctrl+1…9 shows devcontainer 1…9 directly.")
			fmt.Fprintln(out, "# (these keys change nothing outside the weft/dc tmux session)")
			return nil
		},
	}
}
