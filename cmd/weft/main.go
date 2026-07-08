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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "weft: "+err.Error())
		os.Exit(cli.ExitCode(err))
	}
}
