package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// ghosttyDigits are the key names ghostty uses for the digit row.
var ghosttyDigits = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

const (
	dcKeysBegin = "# >>> weft dc keys >>>"
	dcKeysEnd   = "# <<< weft dc keys <<<"
)

// dcKeysSnippet is the ghostty keybind block weft manages, fenced by markers
// so installs are idempotent and uninstalls are clean.
func dcKeysSnippet() string {
	var b strings.Builder
	b.WriteString(dcKeysBegin + "\n")
	b.WriteString("# Ctrl+1..9 switches devcontainers inside the weft/dc tmux session.\n")
	for i, name := range ghosttyDigits {
		fmt.Fprintf(&b, "keybind = ctrl+%s=text:\\x1b[weft%d~\n", name, i+1)
	}
	b.WriteString(dcKeysEnd + "\n")
	return b.String()
}

func newDcKeysCmd() *cobra.Command {
	var install, uninstall, yes bool
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Enable Ctrl+1…9 switching (prints or installs the terminal keybinds)",
		Long: `Ctrl+1…9 jumps straight to the n-th devcontainer, but the terminal has to
send sequences tmux can see. Without flags this prints the keybind lines to
paste into your terminal config; --install writes them for you (with
consent, idempotently, ghostty only for now) and --uninstall removes them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case install:
				return runDcKeysInstall(cmd, yes)
			case uninstall:
				return runDcKeysUninstall(cmd)
			default:
				out := cmd.OutOrStdout()
				fmt.Fprintln(out, "# ghostty — paste into your ghostty config, then reload it (cmd+shift+,)")
				fmt.Fprintln(out, "# (or run `weft dc keys --install` to have weft add it for you)")
				fmt.Fprint(out, dcKeysSnippet())
				return nil
			}
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "append the keybinds to your ghostty config (asks first)")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove the weft-managed keybind block")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

// ghosttyConfigPath finds the ghostty config, preferring an existing file; a
// missing config lands at the XDG location, which ghostty always reads.
func ghosttyConfigPath() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	var cands []string
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		cands = append(cands, filepath.Join(x, "ghostty", "config"))
	}
	cands = append(cands,
		filepath.Join(home, ".config", "ghostty", "config"),
		filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty", "config"),
	)
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return cands[len(cands)-2], nil // ~/.config/ghostty/config
}

// ghosttyPresent reports whether ghostty is plausibly the user's terminal:
// its env vars (unreliable inside tmux) or an existing config file.
func ghosttyPresent() bool {
	if os.Getenv("TERM_PROGRAM") == "ghostty" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	p, err := ghosttyConfigPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func runDcKeysInstall(cmd *cobra.Command, yes bool) error {
	if !ghosttyPresent() {
		return fmt.Errorf("could not detect a ghostty config — run `weft dc keys` and paste the lines into your terminal's config manually")
	}
	path, err := ghosttyConfigPath()
	if err != nil {
		return err
	}
	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, dcKeysBegin) && strings.Contains(content, `[weft1~`) {
		fmt.Fprintf(cmd.OutOrStdout(), "the keybinds already exist in %s (added manually) — nothing to do\n", path)
		return nil
	}

	if !yes {
		fmt.Fprintf(cmd.ErrOrStderr(), "append %d keybind lines to %s? [y/N] ", len(ghosttyDigits), path)
		ans, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if a := strings.TrimSpace(strings.ToLower(ans)); a != "y" && a != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "aborted — nothing written")
			return nil
		}
	}

	updated := dcKeysReplaceBlock(content, dcKeysSnippet())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ installed into %s — reload ghostty (cmd+shift+,) and Ctrl+1…9 is live in weft dc\n", path)
	return nil
}

func runDcKeysUninstall(cmd *cobra.Command) error {
	path, err := ghosttyConfigPath()
	if err != nil {
		return err
	}
	content := ""
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	}
	if !strings.Contains(content, dcKeysBegin) {
		fmt.Fprintln(cmd.OutOrStdout(), "no weft-managed keybind block found — nothing to do")
		return nil
	}
	updated := dcKeysReplaceBlock(content, "")
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ removed the weft keybind block from %s\n", path)
	return nil
}

// dcKeysReplaceBlock swaps the fenced weft block for repl (or appends repl
// when no block exists; an empty repl removes the block).
func dcKeysReplaceBlock(content, repl string) string {
	start := strings.Index(content, dcKeysBegin)
	if start < 0 {
		if repl == "" {
			return content
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + "\n" + repl
	}
	end := strings.Index(content[start:], dcKeysEnd)
	tail := ""
	if end >= 0 {
		tail = content[start+end+len(dcKeysEnd):]
		tail = strings.TrimPrefix(tail, "\n")
	}
	head := strings.TrimRight(content[:start], "\n")
	parts := []string{}
	if head != "" {
		parts = append(parts, head, "")
	}
	if repl != "" {
		parts = append(parts, repl)
	}
	out := strings.Join(parts, "\n") + tail
	return out
}
