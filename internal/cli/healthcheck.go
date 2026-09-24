package cli

import (
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
			// ContractProbePath follows the VEIL_VAR_DIR-derived state root so
			// custom-root containers probe the same file the serve side wrote
			// (HEALTHCHECK execs bypass the entrypoint — issue #753).
			path := statusflow.ContractProbePath()
			contract, err := statusflow.ReadContract(path)
			if err != nil {
				return err
			}
			token, _ := serveflow.NewEnvironment().AuthToken("")
			return statusflow.Probe(cmd.Context(), contract, token)
		},
	}
}
