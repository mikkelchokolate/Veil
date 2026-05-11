package cli

import (
	"context"
	"net/http"

	"github.com/spf13/cobra"
	serveflow "github.com/veil-panel/veil/internal/cliflow/serve"
	statusflow "github.com/veil-panel/veil/internal/cliflow/status"
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
			return statusflow.NewQuery(statusflow.Options{Listen: listen, AuthToken: authToken, JSON: jsonOutput}, cmd.OutOrStdout(), serveflow.NewEnvironment().AuthToken).Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&listen, "listen", "", "veil serve address (default: 127.0.0.1:2096)")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API bearer token")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func resolveStatusListen(flagValue string) string {
	return statusflow.ResolveListen(flagValue)
}

type statusResponse = statusflow.Response
type serviceStatus = statusflow.ServiceStatus

func fetchStatus(ctx context.Context, url string, token string) (*statusResponse, error) {
	return statusflow.Fetch(ctx, url, token)
}

func statusHTTPClient(rawURL string) *http.Client {
	return statusflow.HTTPClient(rawURL)
}

func isLocalStatusHost(host string) bool {
	return statusflow.IsLocalHost(host)
}
