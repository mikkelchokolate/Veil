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
	results := validator.Validate([]string{"/etc/veil/generated/mieru/server_config.json", "/etc/veil/other.txt"})
	if len(results) != 1 || results[0].Name != "mieru" || !strings.Contains(results[0].Config, "server_config.json") {
		t.Fatalf("results = %+v", results)
	}
	if len(commands) != 1 || commands[0][0] != "mieru" {
		t.Fatalf("commands = %+v", commands)
	}
}
