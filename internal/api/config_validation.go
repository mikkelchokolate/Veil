package api

import (
	"context"
	"os/exec"
	"path/filepath"
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
	for _, path := range paths {
		slashPath := filepath.ToSlash(path)
		switch {
		case strings.HasSuffix(slashPath, "/generated/caddy/Caddyfile"):
			results = append(results, v.run("caddy", path, []string{"caddy", "validate", "--config", path}))
		case strings.HasSuffix(slashPath, "/generated/hysteria2/server.yaml"):
			results = append(results, v.run("hysteria2", path, []string{"hysteria", "server", "--config", path, "--check"}))
		case strings.HasSuffix(slashPath, "/generated/sing-box/warp.json"):
			results = append(results, v.run("warp", path, []string{"sing-box", "check", "-c", path}))
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
