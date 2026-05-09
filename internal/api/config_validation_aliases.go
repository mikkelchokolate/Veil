package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type ConfigValidationRunner = generatedconfig.ConfigValidationRunner
type StagedConfigValidator = generatedconfig.StagedConfigValidator
type ConfigValidationSpec = generatedconfig.ConfigValidationSpec
type ConfigValidationCatalog = generatedconfig.ConfigValidationCatalog

func NewStagedConfigValidator(run ConfigValidationRunner) StagedConfigValidator {
	return generatedconfig.NewStagedConfigValidator(run)
}

func NewConfigValidationCatalog() ConfigValidationCatalog {
	return generatedconfig.NewConfigValidationCatalog()
}

func runStagedConfigValidators(paths []string) []ConfigValidationResult {
	return generatedconfig.NewStagedConfigValidator(runFixedConfigValidation).Validate(paths)
}

func runFixedConfigValidation(name string, config string, command []string) ConfigValidationResult {
	return generatedconfig.RunFixedConfigValidation(name, config, command)
}
