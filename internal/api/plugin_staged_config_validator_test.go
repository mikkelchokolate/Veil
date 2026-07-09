package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
)

func TestPluginStagedConfigValidatorSkipsMieruWithoutChecker(t *testing.T) {
	called := false
	validator := newPluginStagedConfigValidator(func(name string, config string, command []string) ConfigValidationResult {
		called = true
		return ConfigValidationResult{Name: name, Config: config, Command: command}
	})

	results := validator.Validate([]string{"/etc/veil/generated/mieru/server_config.json"})
	if len(results) != 0 {
		t.Fatalf("expected no mieru validation, got %+v", results)
	}
	if called {
		t.Fatal("mieru has no standalone checker, validator should not run")
	}
}

func TestPluginStagedConfigValidatorUsesPluginAndWarpSpecs(t *testing.T) {
	calls := []string{}
	validator := newPluginStagedConfigValidator(func(name string, config string, command []string) ConfigValidationResult {
		calls = append(calls, name)
		return ConfigValidationResult{Name: name, Config: config, Command: command, Valid: true}
	})

	results := validator.Validate([]string{
		"/etc/veil/generated/caddy/demo.Caddyfile",
		"/etc/veil/generated/sing-box/warp.json",
	})
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if calls[0] != "caddy" || calls[1] != "warp" {
		t.Fatalf("calls = %+v", calls)
	}
	if results[1].Command[0] != "sing-box" || results[1].Config != "/etc/veil/generated/sing-box/warp.json" {
		t.Fatalf("unexpected warp validation result: %+v", results[1])
	}
}

func TestPluginValidationSpecIgnoresUnknownGeneratedPath(t *testing.T) {
	if _, ok := pluginValidationSpec("/etc/veil/generated/unknown/config.json"); ok {
		t.Fatal("unknown generated config should not have validation spec")
	}
}

func TestPluginValidationSpecUsesHysteriaPluginCommand(t *testing.T) {
	if _, ok := pluginValidationSpec("/etc/veil/generated/hysteria2/demo.yaml"); ok {
		t.Fatal("hysteria2 should have no plugin validation spec; it has no standalone config checker")
	}
}

var _ = generatedconfig.ValidationSpec{}
