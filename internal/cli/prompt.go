package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// confirm asks a yes/no question. It defaults to no (including on EOF, so a
// non-interactive pipe without --yes declines rather than proceeds).
func confirm(cmd *cobra.Command, prompt string) bool {
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
