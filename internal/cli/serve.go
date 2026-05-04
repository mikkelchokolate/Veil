package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/api"
)

const serveDrainTimeout = 5 * time.Second

func newServeCommand(version string) *cobra.Command {
	var listen string
	var authToken string
	var statePath string
	var applyRoot string
	var keyPath string
	var tlsCert string
	var tlsKey string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Veil HTTP API and web panel",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateServeListen(listen); err != nil {
				return err
			}
			token, tokenSource := resolveServeAuthToken(authToken)
			if err := validateServeAuthBinding(listen, tokenSource); err != nil {
				return err
			}
			resolvedStatePath, stateSource := resolveServeStatePath(statePath)
			resolvedApplyRoot, applyRootSource := resolveServeApplyRoot(applyRoot)
			resolvedKeyPath, keySource := resolveServeKeyPath(keyPath)
			tlsEnabled, tlsSource := resolveServeTLS(tlsCert, tlsKey)
			server, stateReloader := newServeHTTPServer(listen, version, token, resolvedStatePath, resolvedApplyRoot, resolvedKeyPath, tlsEnabled, tlsCert, tlsKey)
			tlsLabel := "http"
			if tlsEnabled {
				tlsLabel = "https"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Veil listening on %s://%s\n", tlsLabel, listen)
			fmt.Fprintf(cmd.OutOrStdout(), "State path: %s (%s)\n", resolvedStatePath, stateSource)
			fmt.Fprintf(cmd.OutOrStdout(), "Apply root: %s (%s)\n", resolvedApplyRoot, applyRootSource)
			fmt.Fprintf(cmd.OutOrStdout(), "Key path: %s (%s)\n", resolvedKeyPath, keySource)
			if tlsEnabled {
				fmt.Fprintf(cmd.OutOrStdout(), "TLS: enabled (%s)\n", tlsSource)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "TLS: disabled")
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
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:2096", "HTTP/HTTPS listen address")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API bearer token; defaults to VEIL_API_TOKEN when set")
	cmd.Flags().StringVar(&statePath, "state", "", "management state JSON path; defaults to VEIL_STATE_PATH or /var/lib/veil/state.json")
	cmd.Flags().StringVar(&applyRoot, "apply-root", "", "root for staged apply files; defaults to VEIL_APPLY_ROOT or /etc/veil")
	cmd.Flags().StringVar(&keyPath, "key-path", "", "encryption key file path; defaults to VEIL_KEY_PATH or /etc/veil/state.key")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate file path; enables HTTPS when both --tls-cert and --tls-key are provided")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "TLS private key file path; enables HTTPS when both --tls-cert and --tls-key are provided")
	return cmd
}

func newServeHTTPServer(listen string, version string, authToken string, statePath string, applyRoot string, keyPath string, tlsEnabled bool, tlsCert string, tlsKey string) (*http.Server, api.Reloader) {
	handler, reloader := api.NewRouter(api.ServerInfo{Version: version, Mode: "server", AuthToken: authToken, StatePath: statePath, ApplyRoot: applyRoot, KeyPath: keyPath})
	srv := &http.Server{
		Addr:              listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if tlsEnabled {
		srv.TLSConfig = newServeTLSConfig()
	}
	return srv, reloader
}

// newServeTLSConfig returns a secure TLS configuration for the serve command.
// It enforces TLS 1.2 minimum with modern cipher suites and disables insecure
// features like TLS compression and session tickets.
func newServeTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
	}
}

func resolveServeAuthToken(flagValue string) (token string, source string) {
	if token := strings.TrimSpace(flagValue); token != "" {
		return token, "--auth-token"
	}
	if token := strings.TrimSpace(os.Getenv("VEIL_API_TOKEN")); token != "" {
		return token, "VEIL_API_TOKEN"
	}
	return "", "disabled"
}

func validateServeListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if host == "" {
		return fmt.Errorf("listen address must include a host (e.g. 127.0.0.1:%s or localhost:%s)", port, port)
	}
	if err := validateServePort(port); err != nil {
		return fmt.Errorf("listen address has invalid port %q: %w", port, err)
	}
	return nil
}

func validateServePort(port string) error {
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("must be 1-65535")
	}
	return nil
}

func validateServeAuthBinding(listen string, tokenSource string) error {
	if tokenSource != "disabled" {
		return nil
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if err := validateServePort(port); err != nil {
		return fmt.Errorf("listen address has invalid port %q: %w", port, err)
	}
	ip := net.ParseIP(host)
	if strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("API auth token is required when listening on non-loopback address %s; set --auth-token or VEIL_API_TOKEN", listen)
}

func resolveServeStatePath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--state"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); path != "" {
		return path, "VEIL_STATE_PATH"
	}
	return "/var/lib/veil/state.json", "default"
}

func resolveServeApplyRoot(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--apply-root"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_APPLY_ROOT")); path != "" {
		return path, "VEIL_APPLY_ROOT"
	}
	return "/etc/veil", "default"
}

func resolveServeKeyPath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--key-path"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_KEY_PATH")); path != "" {
		return path, "VEIL_KEY_PATH"
	}
	return "/etc/veil/state.key", "default"
}

// resolveServeTLS determines whether TLS should be enabled.
// Both cert and key must be provided; the caller verifies files exist.
func resolveServeTLS(cert, key string) (enabled bool, source string) {
	if cert != "" && key != "" {
		return true, "--tls-cert / --tls-key"
	}
	if c := strings.TrimSpace(os.Getenv("VEIL_TLS_CERT")); c != "" {
		if k := strings.TrimSpace(os.Getenv("VEIL_TLS_KEY")); k != "" {
			return true, "VEIL_TLS_CERT / VEIL_TLS_KEY"
		}
	}
	return false, ""
}
