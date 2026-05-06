package api

import (
	"path/filepath"
	"testing"
)

func TestStagedConfigValidatorBuildsExpectedCommands(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{}
	validator := NewStagedConfigValidator(func(name string, config string, command []string) ConfigValidationResult {
		commands = append(commands, append([]string(nil), command...))
		return ConfigValidationResult{Name: name, Config: config, Command: command, Valid: true}
	})

	results := validator.Validate([]string{
		filepath.Join(root, "generated", "caddy", "Caddyfile"),
		filepath.Join(root, "generated", "hysteria2", "server.yaml"),
		filepath.Join(root, "generated", "sing-box", "warp.json"),
	})
	if len(results) != 3 || len(commands) != 3 {
		t.Fatalf("results=%+v commands=%+v", results, commands)
	}
	if commands[0][0] != "caddy" || commands[1][0] != "hysteria" || commands[2][0] != "sing-box" {
		t.Fatalf("commands = %+v", commands)
	}
}
