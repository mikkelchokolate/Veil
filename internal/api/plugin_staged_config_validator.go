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
	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		renderer, ok := protocols.AsConfigRenderer(plugin)
		if !ok {
			continue
		}
		spec := renderer.ArtifactSpec()
		if !spec.MatchesGeneratedPath(path) {
			continue
		}
		return spec.ValidationSpec(path)
	}

	// WARP is not an inbound protocol plugin, so it remains a fixed generated
	// artifact with its own validator.
	for _, spec := range generatedconfig.NewArtifactCatalog().All() {
		if spec.Subpath != generatedconfig.WarpConfigSubpath {
			continue
		}
		if spec.MatchesGeneratedPath(path) {
			return spec.ValidationSpec(path)
		}
	}
	return generatedconfig.ValidationSpec{}, false
}
