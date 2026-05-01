package cli

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/api"
)

func newServeCommand(version string) *cobra.Command {
	var listen string
	var authToken string
	var statePath string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Veil HTTP API and web panel",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, _, err := net.SplitHostPort(listen); err != nil {
				return fmt.Errorf("listen address must be host:port: %w", err)
			}
			token, tokenSource := resolveServeAuthToken(authToken)
			resolvedStatePath, stateSource := resolveServeStatePath(statePath)
			server := &http.Server{
				Addr:    listen,
				Handler: api.NewRouter(api.ServerInfo{Version: version, Mode: "server", AuthToken: token, StatePath: resolvedStatePath}),
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Veil listening on %s\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "State path: %s (%s)\n", resolvedStatePath, stateSource)
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
	return cmd
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

func resolveServeStatePath(flagValue string) (path string, source string) {
	if path := strings.TrimSpace(flagValue); path != "" {
		return path, "--state"
	}
	if path := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); path != "" {
		return path, "VEIL_STATE_PATH"
	}
	return "/var/lib/veil/state.json", "default"
}
