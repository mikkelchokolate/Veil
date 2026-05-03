package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/veil-panel/veil/internal/cli"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		handleError(err.Error())
		os.Exit(1)
	}
}

// run executes the root command with signal-aware context.
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := cli.NewRootCommand(version)
	cmd.SetContext(ctx)

	return cmd.Execute()
}

// handleError prints an error message to stderr in veil's format.
func handleError(msg string) {
	fmt.Fprintf(os.Stderr, "veil: %s\n", msg)
}
