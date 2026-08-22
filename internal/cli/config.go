package cli

import (
	"bytes"
	"fmt"
	"os"

	"github.com/mikkelchokolate/Veil/internal/api"
	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	var statePath string
	var keyPath string

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

			env := serveflow.NewEnvironment()
			resolvedPath, _ := env.StatePath(statePath)
			fmt.Fprintf(out, "Validating %s...\n", resolvedPath)

			body, err := os.ReadFile(resolvedPath)
			if err != nil {
				return fmt.Errorf("read state file: %w", err)
			}
			if bytes.Contains(body, []byte(secrets.Prefix)) {
				resolvedKey, _ := env.KeyPath(keyPath)
				keyBody, err := os.ReadFile(resolvedKey)
				if err != nil {
					return fmt.Errorf("read state encryption key: %w", err)
				}
				if len(keyBody) != secrets.KeySize {
					return fmt.Errorf("read state encryption key: expected %d bytes, got %d", secrets.KeySize, len(keyBody))
				}
				var key [secrets.KeySize]byte
				copy(key[:], keyBody)
				cipher, err := secrets.NewCipher(key)
				if err != nil {
					return fmt.Errorf("create state cipher: %w", err)
				}
				snapshot, err := managementstate.NewManagementStateCodec().Decode(body)
				if err != nil {
					return fmt.Errorf("decode encrypted state: %w", err)
				}
				if err := managementstate.DecryptSnapshot(&snapshot, cipher); err != nil {
					return fmt.Errorf("decrypt state: %w", err)
				}
				body, err = managementstate.NewManagementStateCodec().Encode(snapshot)
				if err != nil {
					return fmt.Errorf("encode decrypted state: %w", err)
				}
			}

			result, err := api.NewManagementStateValidation().ValidateBytes(body)
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
	validateCmd.Flags().StringVar(&keyPath, "key-path", "", "encryption key file path (default: /etc/veil/state.key)")
	cmd.AddCommand(validateCmd)
	return cmd
}
