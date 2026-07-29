package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mikkelchokolate/Veil/internal/cli"
)

var (
	version = "dev"
	commit  = "unknown"
)

// osExit is the process exit function used by main. It is overridable so that
// main can be exercised in unit tests without terminating the test process.
var osExit = os.Exit

func main() {
	osExit(run())
}

// run executes the root command with signal-aware context and returns an exit
// code. A non-zero code indicates an error that has already been printed to
// stderr.
func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if os.Getenv("VEIL_SHUTDOWN_ON_STDIN_CLOSE") == "1" {
		go func() {
			buf := make([]byte, 1)
			_, _ = os.Stdin.Read(buf)
			cancel()
		}()
	}

	buildVersion := version
	if commit != "" && commit != "unknown" {
		buildVersion = fmt.Sprintf("%s (%s)", version, commit)
	}
	cmd := cli.NewRootCommand(buildVersion)
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		handleError(err.Error())
		return 1
	}
	return 0
}

// handleError prints an error message to stderr in veil's format.
func handleError(msg string) {
	fmt.Fprintf(os.Stderr, "veil: %s\n", msg)
}
