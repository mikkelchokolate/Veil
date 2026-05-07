package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

type serveWorkflowOptions struct {
	Version     string
	Listen      string
	AuthToken   string
	StatePath   string
	ApplyRoot   string
	KeyPath     string
	TLSCert     string
	TLSKey      string
	WebBasePath string
	AutoTLS     bool
	AutoTLSDir  string
}

func runServeWorkflow(cmd *cobra.Command, opts serveWorkflowOptions) error {
	cfg, err := resolveServeConfig(opts)
	if err != nil {
		return err
	}
	server, stateReloader := newServeHTTPServer(opts.Listen, opts.Version, cfg.Token, cfg.StatePath, cfg.ApplyRoot, cfg.KeyPath, cfg.TLSEnabled, opts.TLSCert, opts.TLSKey, cfg.WebBasePath)
	tlsLabel := "http"
	if cfg.TLSEnabled {
		tlsLabel = "https"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Veil listening on %s://%s\n", tlsLabel, opts.Listen)
	fmt.Fprintf(cmd.OutOrStdout(), "State path: %s (%s)\n", cfg.StatePath, cfg.StateSource)
	fmt.Fprintf(cmd.OutOrStdout(), "Apply root: %s (%s)\n", cfg.ApplyRoot, cfg.ApplyRootSource)
	fmt.Fprintf(cmd.OutOrStdout(), "Key path: %s (%s)\n", cfg.KeyPath, cfg.KeySource)
	if cfg.TLSEnabled {
		fmt.Fprintf(cmd.OutOrStdout(), "TLS: enabled (%s)\n", cfg.TLSSource)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "TLS: disabled")
	}
	if cfg.WebBasePath != "/" {
		fmt.Fprintf(cmd.OutOrStdout(), "Web base path: %s\n", cfg.WebBasePath)
	}
	if cfg.TokenSource == "disabled" {
		fmt.Fprintln(cmd.OutOrStdout(), "API auth: disabled")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "API auth: enabled (%s)\n", cfg.TokenSource)
	}

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

	// Start the server in a goroutine.
	serveErr := make(chan error, 1)
	go func() {
		if cfg.TLSEnabled {
			serveErr <- server.ListenAndServeTLS(opts.TLSCert, opts.TLSKey)
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
