package api

import (
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

type pluginStagedConfigValidator struct {
	run generatedconfig.ConfigValidationRunner
}

func newPluginStagedConfigValidator(run generatedconfig.ConfigValidationRunner) pluginStagedConfigValidator {
	if run == nil {
		run = generatedconfig.RunFixedConfigValidation
	}
	return pluginStagedConfigValidator{run: run}
}

func (v pluginStagedConfigValidator) Validate(paths []string) []ConfigValidationResult {
	results := []ConfigValidationResult{}
	for _, path := range paths {
		validation, ok := pluginValidationSpec(path)
		if !ok {
			continue
		}
		results = append(results, v.run(validation.Name, validation.Config, validation.Command))
	}
	return results
}

func pluginValidationSpec(path string) (generatedconfig.ValidationSpec, bool) {
	catalog := generatedconfig.NewArtifactCatalogFromRegistry(protocols.NewGeneratedConfigRegistry())
	return catalog.ValidationSpec(path)
}
