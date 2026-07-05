package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestRunFixedConfigValidationSkipsMissingCommandMaterial(t *testing.T) {
	empty := RunFixedConfigValidation("test", "/path/to/config", nil)
	if !empty.Skipped {
		t.Fatal("expected skipped for empty command")
	}
	if empty.Error != "validator command is empty" {
		t.Fatalf("expected empty command error, got %q", empty.Error)
	}
	if empty.Name != "test" || empty.Config != "/path/to/config" || empty.Command != nil {
		t.Fatalf("expected material preserved, got %+v", empty)
	}

	missing := RunFixedConfigValidation("sing-box", "/etc/veil/generated/sing-box/warp.json", []string{"nonexistent-validator", "check", "-c", "/etc/veil/generated/sing-box/warp.json"})
	if !missing.Skipped {
		t.Fatal("expected skipped when binary not found")
	}
	if missing.Error != "nonexistent-validator not found; syntax validation skipped" {
		t.Fatalf("expected binary not found error, got %q", missing.Error)
	}
	if missing.Name != "sing-box" || len(missing.Command) != 4 || missing.Command[0] != "nonexistent-validator" {
		t.Fatalf("expected material preserved, got %+v", missing)
	}
}

func TestStagedConfigValidatorBuildsExpectedCommands(t *testing.T) {
	root := t.TempDir()
	commands := [][]string{}
	validator := NewStagedConfigValidator(func(name string, config string, command []string) ConfigValidationResult {
		commands = append(commands, append([]string(nil), command...))
		return ConfigValidationResult{Name: name, Config: config, Command: command, Valid: true}
	})

	results := validator.Validate([]string{
		filepath.Join(root, "generated", "caddy", "config.json"),
		filepath.Join(root, "generated", "hysteria2", "server.yaml"), // no standalone checker: no validation produced
		filepath.Join(root, "generated", "sing-box", "warp.json"),
	})
	if len(results) != 2 || len(commands) != 2 {
		t.Fatalf("results=%+v commands=%+v", results, commands)
	}
	if commands[0][0] != "caddy" || commands[1][0] != "sing-box" {
		t.Fatalf("commands = %+v", commands)
	}
}
