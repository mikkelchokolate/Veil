package cli

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/api"
)

func newServeCommand(version string) *cobra.Command {
	var listen string
	var authToken string
	var statePath string
	var applyRoot string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Veil HTTP API and web panel",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, _, err := net.SplitHostPort(listen); err != nil {
				return fmt.Errorf("listen address must be host:port: %w", err)
			}
			token, tokenSource := resolveServeAuthToken(authToken)
			if err := validateServeAuthBinding(listen, tokenSource); err != nil {
				return err
			}
			resolvedStatePath, stateSource := resolveServeStatePath(statePath)
			resolvedApplyRoot, applyRootSource := resolveServeApplyRoot(applyRoot)
			server := newServeHTTPServer(listen, version, token, resolvedStatePath, resolvedApplyRoot)
			fmt.Fprintf(cmd.OutOrStdout(), "Veil listening on %s\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "State path: %s (%s)\n", resolvedStatePath, stateSource)
			fmt.Fprintf(cmd.OutOrStdout(), "Apply root: %s (%s)\n", resolvedApplyRoot, applyRootSource)
			if tokenSource == "disabled" {
				fmt.Fprintln(cmd.OutOrStdout(), "API auth: disabled")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "API auth: enabled (%s)\n", tokenSource)
			}
			return server.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:2096", "HTTP listen address")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API bearer token; defaults to VEIL_API_TOKEN when set")
	cmd.Flags().StringVar(&statePath, "state", "", "management state JSON path; defaults to VEIL_STATE_PATH or /var/lib/veil/state.json")
	cmd.Flags().StringVar(&applyRoot, "apply-root", "", "root for staged apply files; defaults to VEIL_APPLY_ROOT or /etc/veil")
	return cmd
}

func newServeHTTPServer(listen string, version string, authToken string, statePath string, applyRoot string) *http.Server {
	return &http.Server{
		Addr:              listen,
		Handler:           api.NewRouter(api.ServerInfo{Version: version, Mode: "server", AuthToken: authToken, StatePath: statePath, ApplyRoot: applyRoot}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
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

func validateServeAuthBinding(listen string, tokenSource string) error {
	if tokenSource != "disabled" {
		return nil
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("API auth token is required when listening on non-loopback address %s", listen)
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
