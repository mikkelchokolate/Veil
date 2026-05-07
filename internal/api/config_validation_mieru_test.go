package api

import "testing"

func TestStagedConfigValidatorPlansMieruValidation(t *testing.T) {
	var got ConfigValidationResult
	validator := NewStagedConfigValidator(func(name string, config string, command []string) ConfigValidationResult {
		got = ConfigValidationResult{Name: name, Config: config, Command: command}
		return got
	})
	results := validator.Validate([]string{"/etc/veil/generated/mieru/server_config.json"})
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	if got.Name != "mieru" || got.Config != "/etc/veil/generated/mieru/server_config.json" {
		t.Fatalf("result = %+v", got)
	}
	want := []string{"mieru", "check", "-c", "/etc/veil/generated/mieru/server_config.json"}
	if len(got.Command) != len(want) {
		t.Fatalf("command = %+v", got.Command)
	}
	for i := range want {
		if got.Command[i] != want[i] {
			t.Fatalf("command = %+v, want %+v", got.Command, want)
		}
	}
}
