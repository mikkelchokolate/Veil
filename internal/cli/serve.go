package cli

import (
	"fmt"
	"net"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/api"
)

func newServeCommand(version string) *cobra.Command {
	var listen string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Veil HTTP API and web panel",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, _, err := net.SplitHostPort(listen); err != nil {
				return fmt.Errorf("listen address must be host:port: %w", err)
			}
			server := &http.Server{
				Addr:    listen,
				Handler: api.NewRouter(api.ServerInfo{Version: version, Mode: "server"}),
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Veil listening on %s\n", listen)
			return server.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:2096", "HTTP listen address")
	return cmd
}
