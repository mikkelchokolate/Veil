package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/api"
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

			var snapshot struct {
				Settings      json.RawMessage `json:"settings"`
				Inbounds      json.RawMessage `json:"inbounds"`
				RoutingRules  json.RawMessage `json:"routingRules"`
				RoutingPreset string          `json:"routingPreset,omitempty"`
				Warp          json.RawMessage `json:"warp"`
			}
			if err := json.Unmarshal(body, &snapshot); err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}

			errors := validateStateSnapshot(snapshot)
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
func validateStateSnapshot(snapshot struct {
	Settings      json.RawMessage `json:"settings"`
	Inbounds      json.RawMessage `json:"inbounds"`
	RoutingRules  json.RawMessage `json:"routingRules"`
	RoutingPreset string          `json:"routingPreset,omitempty"`
	Warp          json.RawMessage `json:"warp"`
}) []string {
	var errs []string

	// Validate settings
	if len(snapshot.Settings) > 0 {
		var settings api.Settings
		if err := json.Unmarshal(snapshot.Settings, &settings); err != nil {
			errs = append(errs, fmt.Sprintf("settings: invalid JSON: %v", err))
		} else {
			if settings.PanelListen == "" {
				errs = append(errs, "settings.panelListen is required")
			}
			if settings.Stack == "" {
				errs = append(errs, "settings.stack is required")
			}
			if settings.Mode == "" {
				errs = append(errs, "settings.mode is required")
			}
			if settings.Stack != "naive" && settings.Stack != "hysteria2" && settings.Stack != "both" {
				errs = append(errs, fmt.Sprintf("settings.stack must be naive, hysteria2, or both, got: %s", settings.Stack))
			}
		}
	} else {
		errs = append(errs, "settings is missing")
	}

	// Validate inbounds
	if len(snapshot.Inbounds) > 0 {
		var inbounds []api.Inbound
		if err := json.Unmarshal(snapshot.Inbounds, &inbounds); err != nil {
			errs = append(errs, fmt.Sprintf("inbounds: invalid JSON: %v", err))
		} else {
			seenPorts := map[string]bool{}
			for i, inbound := range inbounds {
				if inbound.Name == "" {
					errs = append(errs, fmt.Sprintf("inbounds[%d].name is required", i))
				}
				if inbound.Protocol == "" {
					errs = append(errs, fmt.Sprintf("inbounds[%d].protocol is required", i))
				}
				if inbound.Transport == "" {
					errs = append(errs, fmt.Sprintf("inbounds[%d].transport is required", i))
				}
				if inbound.Port <= 0 || inbound.Port > 65535 {
					errs = append(errs, fmt.Sprintf("inbounds[%d].port must be 1-65535, got: %d", i, inbound.Port))
				}
				key := inbound.Transport + ":" + fmt.Sprint(inbound.Port)
				if seenPorts[key] {
					errs = append(errs, fmt.Sprintf("inbounds[%d]: duplicate transport/port %s", i, key))
				}
				seenPorts[key] = true
			}
		}
	}

	// Validate routing rules
	if len(snapshot.RoutingRules) > 0 {
		var rules []api.RoutingRule
		if err := json.Unmarshal(snapshot.RoutingRules, &rules); err != nil {
			errs = append(errs, fmt.Sprintf("routingRules: invalid JSON: %v", err))
		} else {
			for i, rule := range rules {
				if rule.Name == "" {
					errs = append(errs, fmt.Sprintf("routingRules[%d].name is required", i))
				}
				if rule.Match == "" {
					errs = append(errs, fmt.Sprintf("routingRules[%d].match is required", i))
				}
				if rule.Outbound == "" {
					errs = append(errs, fmt.Sprintf("routingRules[%d].outbound is required", i))
				}
			}
		}
	}

	// Validate WARP config
	if len(snapshot.Warp) > 0 {
		var warp api.WarpConfig
		if err := json.Unmarshal(snapshot.Warp, &warp); err != nil {
			errs = append(errs, fmt.Sprintf("warp: invalid JSON: %v", err))
		}
	}

	return errs
}
