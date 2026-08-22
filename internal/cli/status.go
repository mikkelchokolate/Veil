package cli

import (
	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
	"github.com/spf13/cobra"
)

func newStatusCommand(version string) *cobra.Command {
	var listen string
	var authToken string
	var webBasePath string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Veil service status",
		Long: `Status queries a running veil serve instance and displays service status.

By default it connects to 127.0.0.1:2096. Use --listen to specify a different
address and --auth-token to authenticate. For a panel mounted below a secret
base path, use --web-base-path or VEIL_WEB_BASE_PATH.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			env := serveflow.NewEnvironment()
			resolvedWebBasePath, _ := env.WebBasePath(webBasePath)
			return statusflow.NewQuery(statusflow.Options{Listen: listen, AuthToken: authToken, WebBasePath: resolvedWebBasePath, JSON: jsonOutput}, cmd.OutOrStdout(), env.AuthToken).Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&listen, "listen", "", "veil serve address (default: 127.0.0.1:2096)")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API bearer token")
	cmd.Flags().StringVar(&webBasePath, "web-base-path", "", "base path prefix for the web panel; defaults to VEIL_WEB_BASE_PATH or /")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
