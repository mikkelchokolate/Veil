package cli

import (
	"fmt"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
	"github.com/spf13/cobra"
)

func newHealthcheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "healthcheck",
		Short:  "Probe the local container health contract",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := statusflow.ContractPathFromEnv()
			if path == "" {
				return fmt.Errorf("VEIL_CONTAINER_HEALTH_PATH is not set")
			}
			contract, err := statusflow.ReadContract(path)
			if err != nil {
				return err
			}
			token, _ := serveflow.NewEnvironment().AuthToken("")
			return statusflow.Probe(cmd.Context(), contract, token)
		},
	}
}
