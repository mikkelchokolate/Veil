package generatedconfig

import "os"

type ConfigValidationRunner func(name string, config string, command []string) ConfigValidationResult

type StagedConfigValidator struct {
	run ConfigValidationRunner
}

func NewStagedConfigValidator(run ConfigValidationRunner) StagedConfigValidator {
	if run == nil {
		run = RunFixedConfigValidation
	}
	return StagedConfigValidator{run: run}
}

func (v StagedConfigValidator) Validate(paths []string) []ConfigValidationResult {
	results := []ConfigValidationResult{}
	catalog := NewConfigValidationCatalog()
	for _, path := range paths {
		validation, ok := catalog.Match(path)
		if ok {
			results = append(results, v.run(validation.Name, validation.Config, validation.Command))
		}
	}
	return results
}

func RunFixedConfigValidation(name string, config string, command []string) ConfigValidationResult {
	result := ConfigValidationResult{Name: name, Config: config, Command: append([]string(nil), command...)}
	// Caddy's internal CA needs a writable home directory; veil's locked user
	// has HOME=/nonexistent, so point validation at the writable state dir.
	env := append(os.Environ(), "HOME=/var/lib/veil", "XDG_DATA_HOME=/var/lib/veil")
	output := NewRuntimeCommandExecutor().Run(RuntimeCommandInput{Command: command, Env: env})
	result.Output = output.Output
	if output.Empty {
		result.Skipped = true
		result.Error = "validator command is empty"
		return result
	}
	if output.NotFound {
		result.Skipped = true
		result.Error = command[0] + " not found; syntax validation skipped"
		return result
	}
	if output.TimedOut {
		result.Error = "validation timed out"
		return result
	}
	if output.Err != nil {
		result.Error = output.Err.Error()
		return result
	}
	result.Valid = true
	return result
}
