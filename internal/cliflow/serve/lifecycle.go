package serve

import (
	"context"
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

func RunLifecycle(opts LifecycleOptions) error {
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
	if opts.StateReloader != nil {
		// SIGHUP reloads management state from disk without restart.
		sighupCh := make(chan os.Signal, 1)
		signal.Notify(sighupCh, syscall.SIGHUP)
		go func() {
			for range sighupCh {
				if err := opts.StateReloader.Reload(); err != nil {
					fmt.Fprintf(opts.Err, "reload error: %v\n", err)
				} else {
					fmt.Fprintln(opts.Out, "State reloaded (SIGHUP)")
				}
			}
		}()
	}

	// Start the server in a goroutine.
	serveErr := make(chan error, 1)
	go func() {
		if opts.TLSEnabled {
			serveErr <- opts.Server.ListenAndServeTLS(opts.TLSCert, opts.TLSKey)
		} else {
			serveErr <- opts.Server.ListenAndServe()
		}
	}()

	// Wait for either a serve error or context cancellation.
	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case <-opts.Context.Done():
		fmt.Fprintln(opts.Out, "Shutting down...")
	}

	// Graceful shutdown with drain timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), opts.DrainTimeout)
	defer shutdownCancel()
	if err := opts.Server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	fmt.Fprintln(opts.Out, "Server stopped")
	return nil
}
