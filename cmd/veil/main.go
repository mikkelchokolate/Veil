package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mikkelchokolate/Veil/internal/cli"
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

	if os.Getenv("VEIL_SHUTDOWN_ON_STDIN_CLOSE") == "1" {
		go func() {
			buf := make([]byte, 1)
			_, _ = os.Stdin.Read(buf)
			cancel()
		}()
	}

	cmd := cli.NewRootCommand(version)
	cmd.SetContext(ctx)

	return cmd.Execute()
}

// handleError prints an error message to stderr in veil's format.
func handleError(msg string) {
	fmt.Fprintf(os.Stderr, "veil: %s\n", msg)
}
