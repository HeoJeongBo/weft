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
		binCheck(ctx, "claude", true, "install the Claude Code CLI: https://claude.com/claude-code", "--version"),
		binCheck(ctx, "node", false, "install Node.js (required by the Dev Container CLI)", "--version"),
	}
}

// binCheck verifies a binary is on PATH and, if so, records its version output.
func binCheck(ctx context.Context, bin string, required bool, hint string, verArgs ...string) checkResult {
	r := checkResult{Name: bin, Required: required, Hint: hint}
	if _, err := exec.LookPath(bin); err != nil {
		r.Detail = "not found on PATH"
		return r
	}
	r.OK = true
	r.Detail = "installed"
	if len(verArgs) > 0 {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(cctx, bin, verArgs...).Output(); err == nil {
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
	if _, err := exec.LookPath("docker"); err != nil {
		r.Detail = "docker not installed"
		return r
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := exec.CommandContext(cctx, "docker", "info").Run(); err != nil {
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
	f, ok := cmd.OutOrStdout().(*os.File)
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
