// Command weft orchestrates parallel Claude Code sessions across git worktrees,
// devcontainers, and tmux.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/HeoJeongBo/weft/internal/cli"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code, ensuring deferred
// cleanup runs before the caller calls os.Exit. fang renders any error itself,
// so we only map it to an exit code here.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.ExitCode(cli.Execute(ctx, args, stdout, stderr))
}
