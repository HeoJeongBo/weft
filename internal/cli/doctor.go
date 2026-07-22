package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/HeoJeongBo/weft/internal/wefterr"
)

// checkResult is the outcome of a single environment check.
type checkResult struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
	Hint     string `json:"hint,omitempty"`
}

func newDoctorCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that weft's dependencies are installed and healthy",
		Long: `doctor verifies the tools weft orchestrates: git, tmux, a Docker daemon,
the Dev Container CLI, and the Claude Code CLI.

It exits non-zero if any required dependency is missing, so it is safe to use in
setup scripts and CI.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			results := runChecks(cmd.Context())
			color := colorEnabled(cmd)
			return reportChecks(cmd.OutOrStdout(), results, jsonOut, color)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output results as JSON")
	return cmd
}

func runChecks(ctx context.Context) []checkResult {
	return []checkResult{
		binCheck(ctx, "git", true, "install git (e.g. brew install git)", "--version"),
		binCheck(ctx, "tmux", true, "install tmux (e.g. brew install tmux)", "-V"),
		binCheck(ctx, "docker", true, "install Docker Desktop, OrbStack, or colima", "--version"),
		dockerDaemonCheck(ctx),
		binCheck(ctx, "devcontainer", true, "npm install -g @devcontainers/cli", "--version"),
		// weft dc installs claude inside each container; a host install is only
		// needed to run claude directly on the host (exec_in_container: false).
		binCheck(ctx, "claude", false, "only needed on the host — weft dc installs claude in-container", "--version"),
		binCheck(ctx, "node", false, "install Node.js (required by the Dev Container CLI)", "--version"),
		tokenCheck(),
		devcontainerScanCheck(ctx),
	}
}

// tokenCheck reports whether the long-lived auth token from `weft dc token`
// exists, so containers skip the per-container OAuth login.
func tokenCheck() checkResult {
	r := checkResult{Name: "dc token", Required: false, Hint: "run `weft dc token` once so containers skip the login screen"}
	home, err := userHomeDir()
	if err != nil {
		r.Detail = "cannot resolve home"
		return r
	}
	if _, err := os.Stat(home + "/.claude/weft-oauth-token"); err != nil {
		r.Detail = "not set up"
		return r
	}
	r.OK = true
	r.Detail = "present"
	return r
}

// devcontainerScanCheck reports how many devcontainers the dc label scan sees.
func devcontainerScanCheck(ctx context.Context) checkResult {
	r := checkResult{Name: "devcontainers", Required: false, Hint: "open one in your editor or run `devcontainer up`"}
	if _, err := lookPath("docker"); err != nil {
		r.Detail = "docker not installed"
		return r
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := runCommand(cctx, "docker", "ps", "-a", "--filter", "label=devcontainer.local_folder", "--format", "{{.ID}}")
	if err != nil {
		r.Detail = "daemon not reachable"
		return r
	}
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			n++
		}
	}
	if n == 0 {
		r.Detail = "none found"
		return r
	}
	r.OK = true
	r.Detail = fmt.Sprintf("%d found (weft dc)", n)
	return r
}

// Seams so doctor's environment probing can be scripted in tests.
var (
	lookPath   = exec.LookPath
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
	runOK = func(ctx context.Context, name string, args ...string) error {
		return exec.CommandContext(ctx, name, args...).Run()
	}
)

// binCheck verifies a binary is on PATH and, if so, records its version output.
func binCheck(ctx context.Context, bin string, required bool, hint string, verArgs ...string) checkResult {
	r := checkResult{Name: bin, Required: required, Hint: hint}
	if _, err := lookPath(bin); err != nil {
		r.Detail = "not found on PATH"
		return r
	}
	r.OK = true
	r.Detail = "installed"
	if len(verArgs) > 0 {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if out, err := runCommand(cctx, bin, verArgs...); err == nil {
			if v := strings.TrimSpace(firstLine(string(out))); v != "" {
				r.Detail = v
			}
		}
	}
	return r
}

// dockerDaemonCheck verifies a Docker daemon is reachable.
func dockerDaemonCheck(ctx context.Context) checkResult {
	r := checkResult{Name: "docker daemon", Required: true, Hint: "start Docker Desktop / OrbStack / colima"}
	if _, err := lookPath("docker"); err != nil {
		r.Detail = "docker not installed"
		return r
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := runOK(cctx, "docker", "info"); err != nil {
		r.Detail = "not reachable"
		return r
	}
	r.OK = true
	r.Detail = "reachable"
	return r
}

func reportChecks(w io.Writer, results []checkResult, jsonOut, color bool) error {
	var missing int
	for _, r := range results {
		if r.Required && !r.OK {
			missing++
		}
	}

	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			fmt.Fprintf(w, "%s %-14s %s\n", symbol(r, color), r.Name, r.Detail)
			if !r.OK && r.Hint != "" {
				fmt.Fprintf(w, "    %s→ %s%s\n", paint("\x1b[2m", color), r.Hint, paint("\x1b[0m", color))
			}
		}
		fmt.Fprintln(w)
		if missing == 0 {
			fmt.Fprintf(w, "%s all required dependencies present\n", paint("\x1b[32m✓\x1b[0m", color))
		} else {
			fmt.Fprintf(w, "%s %d required dependenc%s missing\n",
				paint("\x1b[31m✗\x1b[0m", color), missing, plural(missing, "y", "ies"))
		}
	}

	if missing > 0 {
		return fmt.Errorf("%w (%d missing)", wefterr.ErrDependencyMissing, missing)
	}
	return nil
}

func symbol(r checkResult, color bool) string {
	switch {
	case r.OK:
		return paint("\x1b[32m✓\x1b[0m", color)
	case r.Required:
		return paint("\x1b[31m✗\x1b[0m", color)
	default:
		return paint("\x1b[33m○\x1b[0m", color)
	}
}

// colorEnabled reports whether colored output should be used.
func colorEnabled(cmd *cobra.Command) bool {
	if noColor, _ := cmd.Flags().GetBool("no-color"); noColor {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return isTerminal(cmd.OutOrStdout())
}

// isTerminal reports whether w is an interactive terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func paint(s string, color bool) string {
	if color {
		return s
	}
	// Strip ANSI escapes when color is disabled.
	return stripANSI(s)
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
