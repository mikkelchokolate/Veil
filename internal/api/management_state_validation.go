package api

import (
	"encoding/json"
	"strconv"
)

type ManagementStateValidation struct{}

type ManagementStateValidationResult struct {
	Valid  bool
	Errors []string
}

func NewManagementStateValidation() ManagementStateValidation { return ManagementStateValidation{} }

func (v ManagementStateValidation) ValidateBytes(body []byte) (ManagementStateValidationResult, error) {
	snapshot, err := NewManagementStateCodec().Decode(body)
	if err != nil {
		if syntaxErr := managementStateDecodeError(err); syntaxErr != nil {
			return ManagementStateValidationResult{}, syntaxErr
		}
		errs := []string{"state: invalid JSON: " + err.Error()}
		return ManagementStateValidationResult{Valid: false, Errors: errs}, nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return ManagementStateValidationResult{}, err
	}
	errs := v.ValidateSnapshot(snapshot, fields)
	return ManagementStateValidationResult{Valid: len(errs) == 0, Errors: errs}, nil
}

func (ManagementStateValidation) ValidateSnapshot(snapshot managementSnapshot, fields map[string]json.RawMessage) []string {
	var errs []string
	if _, ok := fields["settings"]; ok {
		if snapshot.Settings.PanelListen == "" {
			errs = append(errs, "settings.panelListen is required")
		}
		if snapshot.Settings.Mode == "" {
			errs = append(errs, "settings.mode is required")
		}
	} else {
		errs = append(errs, "settings is missing")
	}
	if _, ok := fields["inbounds"]; ok {
		seenPorts := map[string]bool{}
		for i, inbound := range snapshot.Inbounds {
			if inbound.Name == "" {
				errs = append(errs, "inbounds["+itoa(i)+"].name is required")
			}
			if inbound.Protocol == "" {
				errs = append(errs, "inbounds["+itoa(i)+"].protocol is required")
			}
			if inbound.Transport == "" {
				errs = append(errs, "inbounds["+itoa(i)+"].transport is required")
			}
			if inbound.Port <= 0 || inbound.Port > 65535 {
				errs = append(errs, "inbounds["+itoa(i)+"].port must be 1-65535, got: "+itoa(inbound.Port))
			}
			key := inbound.Transport + ":" + itoa(inbound.Port)
			if seenPorts[key] {
				errs = append(errs, "inbounds["+itoa(i)+"]: duplicate transport/port "+key)
			}
			seenPorts[key] = true
		}
	}
	if _, ok := fields["routingRules"]; ok {
		for i, rule := range snapshot.Rules {
			if rule.Name == "" {
				errs = append(errs, "routingRules["+itoa(i)+"].name is required")
			}
			if rule.Match == "" {
				errs = append(errs, "routingRules["+itoa(i)+"].match is required")
			}
			if rule.Outbound == "" {
				errs = append(errs, "routingRules["+itoa(i)+"].outbound is required")
			}
		}
	}
	if _, ok := fields["settings"]; ok {
		plan := NewManagementApplyIntent(ManagementApplyIntentInput{Settings: snapshot.Settings, Inbounds: snapshot.Inbounds, Rules: snapshot.Rules, Warp: snapshot.Warp, SkipRenderCheck: true}).BuildPlan()
		for _, err := range plan.Errors {
			errs = appendUnique(errs, err)
		}
	}
	return errs
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
