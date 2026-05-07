package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/api"
)

func runServeLifecycle(cmd *cobra.Command, server *http.Server, stateReloader api.Reloader, tlsEnabled bool, tlsCert string, tlsKey string) error {
	if stateReloader != nil {
		// SIGHUP reloads management state from disk without restart.
		sighupCh := make(chan os.Signal, 1)
		signal.Notify(sighupCh, syscall.SIGHUP)
		go func() {
			for range sighupCh {
				if err := stateReloader.Reload(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "reload error: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "State reloaded (SIGHUP)")
				}
			}
		}()
	}

	// Start the server in a goroutine.
	serveErr := make(chan error, 1)
	go func() {
		if tlsEnabled {
			serveErr <- server.ListenAndServeTLS(tlsCert, tlsKey)
		} else {
			serveErr <- server.ListenAndServe()
		}
	}()

	// Wait for either a serve error or context cancellation.
	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case <-cmd.Context().Done():
		fmt.Fprintln(cmd.OutOrStdout(), "Shutting down...")
	}

	// Graceful shutdown with drain timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), serveDrainTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Server stopped")
	return nil
}
