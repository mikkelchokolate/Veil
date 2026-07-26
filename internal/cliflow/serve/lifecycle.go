package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikkelchokolate/Veil/internal/api"
)

type LifecycleOptions struct {
	Context       context.Context
	Out           io.Writer
	Err           io.Writer
	Server        *http.Server
	StateReloader api.Reloader
	TLSEnabled    bool
	TLSCert       string
	TLSKey        string
	DrainTimeout  time.Duration
}

// lifecycleListenAndServe and lifecycleListenAndServeTLS are overridable
// hooks so tests can mock server startup without binding real ports.
var (
	lifecycleListenAndServe    = func(srv *http.Server) error { return srv.ListenAndServe() }
	lifecycleListenAndServeTLS = func(srv *http.Server, certFile, keyFile string) error {
		return srv.ListenAndServeTLS(certFile, keyFile)
	}
)

func RunLifecycle(opts LifecycleOptions) (result error) {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Err == nil {
		opts.Err = io.Discard
	}
	if opts.DrainTimeout == 0 {
		opts.DrainTimeout = 5 * time.Second
	}
	if closer, ok := opts.StateReloader.(interface{ Close() error }); ok {
		defer func() {
			if err := closer.Close(); err != nil {
				closeErr := fmt.Errorf("close state lifecycle: %w", err)
				if result == nil {
					result = closeErr
				} else {
					result = errors.Join(result, closeErr)
				}
			}
		}()
	}
	var sighupCh chan os.Signal
	if opts.StateReloader != nil {
		// SIGHUP reloads management state from disk without restart.
		sighupCh = make(chan os.Signal, 1)
		signal.Notify(sighupCh, syscall.SIGHUP)
		defer signal.Stop(sighupCh)
	}

	// Start the server in a goroutine.
	serveErr := make(chan error, 1)
	go func() {
		if opts.TLSEnabled {
			serveErr <- lifecycleListenAndServeTLS(opts.Server, opts.TLSCert, opts.TLSKey)
		} else {
			serveErr <- lifecycleListenAndServe(opts.Server)
		}
	}()

	// Wait for either a serve error, context cancellation, or SIGHUP reloads.
	for {
		select {
		case err := <-serveErr:
			if err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("server error: %w", err)
			}
			return nil
		case <-opts.Context.Done():
			fmt.Fprintln(opts.Out, "Shutting down...")
			// Graceful shutdown with drain timeout.
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), opts.DrainTimeout)
			if err := opts.Server.Shutdown(shutdownCtx); err != nil {
				shutdownCancel()
				return fmt.Errorf("shutdown error: %w", err)
			}
			shutdownCancel()
			// Wait for the server goroutine to finish before returning so callers
			// can safely mutate shared test hooks such as lifecycleListenAndServe.
			<-serveErr
			fmt.Fprintln(opts.Out, "Server stopped")
			return nil
		case <-sighupCh:
			if err := opts.StateReloader.Reload(); err != nil {
				fmt.Fprintf(opts.Err, "reload error: %v\n", err)
			} else {
				fmt.Fprintln(opts.Out, "State reloaded (SIGHUP)")
			}
		}
	}
}
