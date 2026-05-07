package api

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

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
	result := ConfigValidationResult{Name: name, Config: config, Command: command}
	if len(command) == 0 {
		result.Skipped = true
		result.Error = "validator command is empty"
		return result
	}
	binary, err := exec.LookPath(command[0])
	if err != nil {
		result.Skipped = true
		result.Error = command[0] + " not found; syntax validation skipped"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command[1:]...)
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = "validation timed out"
		return result
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Valid = true
	return result
}
