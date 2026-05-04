package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCommand(version string) *cobra.Command {
	var listen string
	var authToken string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Veil service status",
		Long: `Status queries a running veil serve instance and displays service status.

By default it connects to 127.0.0.1:2096. Use --listen to specify a different
address and --auth-token to authenticate.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			// Resolve listen address
			addr := resolveStatusListen(listen)
			if !strings.Contains(addr, "://") {
				addr = "http://" + addr
			}

			// Resolve auth token
			token, _ := resolveServeAuthToken(authToken)

			// Fetch status
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			status, err := fetchStatus(ctx, addr+"/api/status", token)
			if err != nil {
				return fmt.Errorf("fetch status from %s: %w", addr, err)
			}

			if jsonOutput {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}

			// Human-readable output
			fmt.Fprintf(out, "Veil %s\n", status.Version)
			fmt.Fprintf(out, "Mode: %s\n", status.Mode)
			fmt.Fprintln(out, "Services:")
			for _, svc := range status.Services {
				state := svc.ActiveState
				if svc.Error != "" {
					state = fmt.Sprintf("%s (error: %s)", state, svc.Error)
				}
				marker := "○"
				if svc.ActiveState == "active" {
					marker = "●"
				} else if svc.ActiveState == "failed" {
					marker = "✕"
				}
				proto := ""
				if svc.Transport != "" {
					proto = fmt.Sprintf(" (%s)", svc.Transport)
				}
				fmt.Fprintf(out, "  %s %s%s: %s\n", marker, svc.Name, proto, state)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&listen, "listen", "", "veil serve address (default: 127.0.0.1:2096)")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API bearer token")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func resolveStatusListen(flagValue string) string {
	if addr := strings.TrimSpace(flagValue); addr != "" {
		return addr
	}
	return "127.0.0.1:2096"
}

type statusResponse struct {
	SchemaVersion string          `json:"schemaVersion"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Mode          string          `json:"mode"`
	Services      []serviceStatus `json:"services"`
}

type serviceStatus struct {
	Name        string `json:"name"`
	Managed     bool   `json:"managed"`
	Transport   string `json:"transport,omitempty"`
	Unit        string `json:"unit,omitempty"`
	LoadState   string `json:"loadState,omitempty"`
	ActiveState string `json:"activeState,omitempty"`
	SubState    string `json:"subState,omitempty"`
	Error       string `json:"error,omitempty"`
}

func fetchStatus(ctx context.Context, url string, token string) (*statusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("X-Veil-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}
