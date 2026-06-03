package api

import (
	"encoding/json"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

type ManagementStateValidation struct{}

type ManagementStateValidationResult = managementstate.ValidationResult

func NewManagementStateValidation() ManagementStateValidation { return ManagementStateValidation{} }

func (v ManagementStateValidation) ValidateBytes(body []byte) (ManagementStateValidationResult, error) {
	result, _, _, err := v.validate(body)
	return result, err
}

func (v ManagementStateValidation) ValidateSnapshot(snapshot managementSnapshot, fields map[string]json.RawMessage) []string {
	errs := managementstate.NewValidation().ValidateSnapshot(snapshot, fields)
	if _, ok := fields["settings"]; ok {
		plan := NewManagementApplyIntent(ManagementApplyIntentInput{Settings: snapshot.Settings, Inbounds: snapshot.Inbounds, Rules: snapshot.Rules, Warp: snapshot.Warp, SkipRenderCheck: true}).BuildPlan()
		for _, err := range plan.Errors {
			errs = managementstate.AppendUnique(errs, err)
		}
	}
	return errs
}

func (v ManagementStateValidation) validate(body []byte) (ManagementStateValidationResult, managementSnapshot, map[string]json.RawMessage, error) {
	snapshot, err := managementstate.NewManagementStateCodec().Decode(body)
	if err != nil {
		if syntaxErr := managementstate.DecodeError(err); syntaxErr != nil {
			return ManagementStateValidationResult{}, managementSnapshot{}, nil, syntaxErr
		}
		return ManagementStateValidationResult{Valid: false, Errors: []string{"state: invalid JSON: " + err.Error()}}, managementSnapshot{}, nil, nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return ManagementStateValidationResult{}, managementSnapshot{}, nil, err
	}
	errs := v.ValidateSnapshot(snapshot, fields)
	return ManagementStateValidationResult{Valid: len(errs) == 0, Errors: errs}, snapshot, fields, nil
}
