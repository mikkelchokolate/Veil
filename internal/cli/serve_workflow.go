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
	if err := validateServeListen(opts.Listen); err != nil {
		return err
	}
	token, tokenSource := resolveServeAuthToken(opts.AuthToken)
	if err := validateServeAuthBinding(opts.Listen, tokenSource); err != nil {
		return err
	}
	resolvedStatePath, stateSource := resolveServeStatePath(opts.StatePath)
	resolvedApplyRoot, applyRootSource := resolveServeApplyRoot(opts.ApplyRoot)
	resolvedKeyPath, keySource := resolveServeKeyPath(opts.KeyPath)
	resolvedWebBasePath, _ := resolveServeWebBasePath(opts.WebBasePath)
	tlsEnabled, tlsSource := resolveServeTLS(opts.TLSCert, opts.TLSKey)
	if opts.AutoTLS && !tlsEnabled {
		autoTLSEnabled, autoTLSErr := resolveServeAutoTLS(opts.AutoTLS, opts.AutoTLSDir, resolvedStatePath, resolvedKeyPath)
		if autoTLSErr != nil {
			return fmt.Errorf("auto-tls: %w", autoTLSErr)
		}
		tlsEnabled = autoTLSEnabled
		tlsSource = "auto-tls (Let's Encrypt)"
	}
	server, stateReloader := newServeHTTPServer(opts.Listen, opts.Version, token, resolvedStatePath, resolvedApplyRoot, resolvedKeyPath, tlsEnabled, opts.TLSCert, opts.TLSKey, resolvedWebBasePath)
	tlsLabel := "http"
	if tlsEnabled {
		tlsLabel = "https"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Veil listening on %s://%s\n", tlsLabel, opts.Listen)
	fmt.Fprintf(cmd.OutOrStdout(), "State path: %s (%s)\n", resolvedStatePath, stateSource)
	fmt.Fprintf(cmd.OutOrStdout(), "Apply root: %s (%s)\n", resolvedApplyRoot, applyRootSource)
	fmt.Fprintf(cmd.OutOrStdout(), "Key path: %s (%s)\n", resolvedKeyPath, keySource)
	if tlsEnabled {
		fmt.Fprintf(cmd.OutOrStdout(), "TLS: enabled (%s)\n", tlsSource)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "TLS: disabled")
	}
	if resolvedWebBasePath != "/" {
		fmt.Fprintf(cmd.OutOrStdout(), "Web base path: %s\n", resolvedWebBasePath)
	}
	if tokenSource == "disabled" {
		fmt.Fprintln(cmd.OutOrStdout(), "API auth: disabled")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "API auth: enabled (%s)\n", tokenSource)
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
		if tlsEnabled {
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
