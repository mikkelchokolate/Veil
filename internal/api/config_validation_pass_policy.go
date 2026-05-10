package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type ConfigValidationPassPolicy = generatedconfig.ValidationPassPolicy

func NewConfigValidationPassPolicy() ConfigValidationPassPolicy {
	return generatedconfig.NewValidationPassPolicy()
}

func requirePassedValidations(validations []ConfigValidationResult) error {
	return generatedconfig.NewValidationPassPolicy().RequirePassed(validations)
}
