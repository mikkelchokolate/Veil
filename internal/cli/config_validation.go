package cli

import "github.com/veil-panel/veil/internal/api"

type ConfigValidation struct{}

type ConfigValidationResult struct {
	Valid  bool
	Errors []string
}

func NewConfigValidation() ConfigValidation { return ConfigValidation{} }

func (ConfigValidation) ValidateBytes(body []byte) (ConfigValidationResult, error) {
	result, err := api.NewManagementStateValidation().ValidateBytes(body)
	if err != nil {
		return ConfigValidationResult{}, err
	}
	return ConfigValidationResult{Valid: result.Valid, Errors: result.Errors}, nil
}
