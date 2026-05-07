package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	var statePath string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Veil configuration",
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a management state file",
		Long:  "Validate reads a Veil management state JSON file and checks it for structural correctness without starting a server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			resolvedPath := resolveConfigPath(statePath)
			fmt.Fprintf(out, "Validating %s...\n", resolvedPath)

			body, err := os.ReadFile(resolvedPath)
			if err != nil {
				return fmt.Errorf("read state file: %w", err)
			}

			result, err := NewConfigValidation().ValidateBytes(body)
			if err != nil {
				return err
			}
			errors := result.Errors
			if len(errors) > 0 {
				fmt.Fprintln(out, "Validation errors:")
				for _, e := range errors {
					fmt.Fprintf(out, "  - %s\n", e)
				}
				return fmt.Errorf("validation failed with %d error(s)", len(errors))
			}

			fmt.Fprintln(out, "Valid.")
			return nil
		},
	}

	validateCmd.Flags().StringVar(&statePath, "state", "", "management state JSON path (default: /var/lib/veil/state.json)")
	cmd.AddCommand(validateCmd)
	return cmd
}

func resolveConfigPath(flagValue string) string {
	if path := flagValue; path != "" {
		return path
	}
	if path := os.Getenv("VEIL_STATE_PATH"); path != "" {
		return path
	}
	return "/var/lib/veil/state.json"
}

// validateStateSnapshot performs structural validation of a management state snapshot.
// This is a best-effort offline check — it does not validate rendered configs.
type configStateSnapshot struct {
	Settings      json.RawMessage `json:"settings"`
	Inbounds      json.RawMessage `json:"inbounds"`
	RoutingRules  json.RawMessage `json:"routingRules"`
	RoutingPreset string          `json:"routingPreset,omitempty"`
	Warp          json.RawMessage `json:"warp"`
}

func validateStateSnapshot(snapshot configStateSnapshot) []string {
	return NewConfigValidation().ValidateSnapshot(snapshot)
}
