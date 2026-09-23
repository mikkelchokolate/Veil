package generatedconfig

import (
	"strings"
	"testing"
)

func TestStagedValidatorMatchesGeneratedConfigCatalog(t *testing.T) {
	var commands [][]string
	validator := NewStagedConfigValidator(func(name, config string, command []string) ConfigValidationResult {
		commands = append(commands, command)
		return ConfigValidationResult{Name: name, Config: config, Command: command, Valid: true}
	})
	// The managed caddy artifact is the consolidated JSON config (#855); a
	// legacy Caddyfile under caddy/ must not match the validation spec.
	results := validator.Validate([]string{
		"/etc/veil/generated/caddy/config.json",
		"/etc/veil/generated/caddy/panel.Caddyfile",
		"/etc/veil/other.txt",
	})
	if len(results) != 1 || results[0].Name != "caddy" || !strings.HasSuffix(results[0].Config, "caddy/config.json") {
		t.Fatalf("results = %+v", results)
	}
	if len(commands) != 1 || commands[0][0] != "caddy" || commands[0][len(commands[0])-1] != "/etc/veil/generated/caddy/config.json" {
		t.Fatalf("commands = %+v", commands)
	}
}
