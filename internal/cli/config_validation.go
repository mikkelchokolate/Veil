package cli

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/veil-panel/veil/internal/api"
)

type ConfigValidation struct{}

type ConfigValidationResult struct {
	Valid  bool
	Errors []string
}

func NewConfigValidation() ConfigValidation { return ConfigValidation{} }

func (ConfigValidation) ValidateBytes(body []byte) (ConfigValidationResult, error) {
	var snapshot configStateSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return ConfigValidationResult{}, fmt.Errorf("invalid JSON: %w", err)
	}
	errs := NewConfigValidation().ValidateSnapshot(snapshot)
	return ConfigValidationResult{Valid: len(errs) == 0, Errors: errs}, nil
}

func (ConfigValidation) ValidateSnapshot(snapshot configStateSnapshot) []string {
	var errs []string
	if len(snapshot.Settings) > 0 {
		var settings api.Settings
		if err := decodeConfigSettings(snapshot.Settings, &settings); err != nil {
			errs = append(errs, fmt.Sprintf("settings: invalid JSON: %v", err))
		} else {
			if settings.PanelListen == "" {
				errs = append(errs, "settings.panelListen is required")
			}
			if settings.Mode == "" {
				errs = append(errs, "settings.mode is required")
			}
		}
	} else {
		errs = append(errs, "settings is missing")
	}
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
	if len(snapshot.Warp) > 0 {
		var warp api.WarpConfig
		if err := json.Unmarshal(snapshot.Warp, &warp); err != nil {
			errs = append(errs, fmt.Sprintf("warp: invalid JSON: %v", err))
		}
	}
	return errs
}

func decodeConfigSettings(body []byte, settings *api.Settings) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(settings)
}
