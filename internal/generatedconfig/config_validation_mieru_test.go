package generatedconfig

import "testing"

func TestStagedConfigValidatorSkipsMieruValidation(t *testing.T) {
	// mieru's server binary (mita) has no standalone config checker, so the
	// mieru config produces no validation step (it is gated by the service
	// health check after restart instead).
	called := false
	validator := NewStagedConfigValidator(func(name string, config string, command []string) ConfigValidationResult {
		called = true
		return ConfigValidationResult{Name: name, Config: config, Command: command}
	})
	results := validator.Validate([]string{"/etc/veil/generated/mieru/server_config.json"})
	if len(results) != 0 {
		t.Fatalf("expected no mieru validation, got %+v", results)
	}
	if called {
		t.Fatal("no validator command should run for mieru")
	}
}
