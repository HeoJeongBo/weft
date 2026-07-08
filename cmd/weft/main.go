// Command weft orchestrates parallel Claude Code sessions across git worktrees,
// devcontainers, and tmux.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/HeoJeongBo/weft/internal/cli"
)

func main() {
	os.Exit(run())
}

// run executes the CLI and returns the process exit code, ensuring deferred
// cleanup runs before the caller calls os.Exit.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "weft: "+err.Error())
		return cli.ExitCode(err)
	}
	return 0
}
