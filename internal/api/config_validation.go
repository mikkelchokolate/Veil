package api

type ConfigValidationRunner func(name string, config string, command []string) ConfigValidationResult

type StagedConfigValidator struct {
	run ConfigValidationRunner
}

func NewStagedConfigValidator(run ConfigValidationRunner) StagedConfigValidator {
	if run == nil {
		run = runFixedConfigValidation
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

func runStagedConfigValidators(paths []string) []ConfigValidationResult {
	return NewStagedConfigValidator(runFixedConfigValidation).Validate(paths)
}

func runFixedConfigValidation(name string, config string, command []string) ConfigValidationResult {
	result := ConfigValidationResult{Name: name, Config: config, Command: append([]string(nil), command...)}
	output := NewRuntimeCommandExecutor().Run(RuntimeCommandInput{Command: command})
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
