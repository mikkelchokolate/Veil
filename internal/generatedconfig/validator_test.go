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
	results := validator.Validate([]string{"/etc/veil/generated/caddy/config.json", "/etc/veil/other.txt"})
	if len(results) != 1 || results[0].Name != "caddy" || !strings.Contains(results[0].Config, "config.json") {
		t.Fatalf("results = %+v", results)
	}
	if len(commands) != 1 || commands[0][0] != "caddy" {
		t.Fatalf("commands = %+v", commands)
	}
}
