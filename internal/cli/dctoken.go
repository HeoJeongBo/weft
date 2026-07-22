package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/devcontainer"
)

func newDcTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token [query]",
		Short: "Mint a long-lived claude token so rebuilt containers stop asking to log in",
		Long: `Run "claude setup-token" inside a running devcontainer (one browser
authorization), then store the token at ~/.claude/weft-oauth-token. Every
weft dc pane exports it as CLAUDE_CODE_OAUTH_TOKEN, so freshly built
containers skip the per-container OAuth login.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDcToken,
	}
}

func runDcToken(cmd *cobra.Command, args []string) error {
	q := strings.Join(args, "")
	r, _, _ := dcRunner(cmd)
	cands, err := dcScan(cmd.Context(), r)
	if err != nil {
		return err
	}
	var running []dcCandidate
	for _, c := range matchDc(cands, q) {
		if c.State == "running" {
			running = append(running, c)
		}
	}
	if len(running) == 0 {
		if q == "" {
			return fmt.Errorf("no running devcontainers — start one first (weft dc <name> --start) and retry")
		}
		return fmt.Errorf("no running devcontainer matches %q — start it first (weft dc %s --start)", q, q)
	}
	if len(running) > 1 {
		printDcTable(cmd.OutOrStdout(), running, colorEnabled(cmd))
		return fmt.Errorf("%d running devcontainers match — pick one by name", len(running))
	}
	c := running[0]

	if home, err := userHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".claude", "weft-oauth-token")); err == nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: a token already exists — completing this flow replaces it")
		}
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "%s claude setup-token (%s) — complete the browser authorization\n",
		colorize("▶", ansiCyan, colorEnabled(cmd)), c.Name)
	argv := devcontainer.ExecArgs(devcontainer.ExecOpts{
		WorkspaceFolder: c.Folder,
		ConfigPath:      c.ConfigPath,
	}, "sh", "-lc", `export PATH="$HOME/.local/bin:$PATH"; export CLAUDE_CONFIG_DIR="$HOME/.claude"; claude setup-token`)
	ex := execCommand(cmd.Context(), argv[0], argv[1:]...)
	ex.Stdin, ex.Stdout, ex.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := ex.Run(); err != nil {
		return err
	}

	fmt.Fprint(cmd.ErrOrStderr(), "paste the token printed above (sk-ant-oat…): ")
	tok, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return fmt.Errorf("no token pasted — nothing saved")
	}
	home, err := userHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".claude", "weft-oauth-token")
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "saved %s — weft dc panes now log in with it automatically\n", path)
	return nil
}
