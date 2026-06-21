package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fatecannotbealtered/archery-cli/cmd"
)

func main() {
	// Trap SIGINT/SIGTERM: cancelling the root context unwinds in-flight work
	// (e.g. a self-update download) to a clean state so the command can still
	// emit the terminal JSON envelope on stdout instead of dying as a bare
	// killed process (CLI-SPEC §14).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_ = cmd.ExecuteContext(ctx)
	os.Exit(cmd.LastExitCode())
}
