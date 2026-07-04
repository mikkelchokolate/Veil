package generatedconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeRuntimeCommandExecutor struct {
	outputs map[string]RuntimeCommandOutput
}

func (e *fakeRuntimeCommandExecutor) Run(input RuntimeCommandInput) RuntimeCommandOutput {
	key := ""
	if len(input.Command) > 0 {
		key = input.Command[0]
	}
	if out, ok := e.outputs[key]; ok {
		return out
	}
	return RuntimeCommandOutput{Command: input.Command, Err: errors.New("unexpected command")}
}

func TestNewStagedConfigValidatorUsesDefaultRunnerWhenNil(t *testing.T) {
	validator := NewStagedConfigValidator(nil)
	// mieru has no standalone checker, so the default runner should not be invoked.
	results := validator.Validate([]string{"/etc/veil/generated/mieru/server_config.json"})
	if len(results) != 0 {
		t.Fatalf("expected no results for mieru, got %+v", results)
	}
}

func TestRunFixedConfigValidationMarksValidResult(t *testing.T) {
	old := runtimeCommandExecutor
	t.Cleanup(func() { runtimeCommandExecutor = old })
	runtimeCommandExecutor = &fakeRuntimeCommandExecutor{
		outputs: map[string]RuntimeCommandOutput{
			"caddy": {Command: []string{"caddy"}, Output: "valid"},
		},
	}

	result := RunFixedConfigValidation("caddy", "/etc/veil/generated/caddy/Caddyfile", []string{"caddy", "validate", "--config", "/etc/veil/generated/caddy/Caddyfile"})
	if !result.Valid {
		t.Fatalf("expected valid result, got %+v", result)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %q", result.Error)
	}
}

func TestRunFixedConfigValidationCapturesCommandError(t *testing.T) {
	old := runtimeCommandExecutor
	t.Cleanup(func() { runtimeCommandExecutor = old })
	runtimeCommandExecutor = &fakeRuntimeCommandExecutor{
		outputs: map[string]RuntimeCommandOutput{
			"sing-box": {Command: []string{"sing-box"}, Err: errors.New("bad yaml"), Output: "oops"},
		},
	}

	result := RunFixedConfigValidation("sing-box", "/etc/veil/generated/sing-box/warp.json", []string{"sing-box", "check", "-c", "/etc/veil/generated/sing-box/warp.json"})
	if result.Valid || result.Error != "bad yaml" {
		t.Fatalf("expected command error, got %+v", result)
	}
}

func TestRunFixedConfigValidationCapturesTimeout(t *testing.T) {
	old := runtimeCommandExecutor
	t.Cleanup(func() { runtimeCommandExecutor = old })
	runtimeCommandExecutor = &fakeRuntimeCommandExecutor{
		outputs: map[string]RuntimeCommandOutput{
			"caddy": {Command: []string{"caddy"}, TimedOut: true, Err: errors.New("context deadline exceeded")},
		},
	}

	result := RunFixedConfigValidation("caddy", "/etc/veil/generated/caddy/Caddyfile", []string{"caddy", "validate", "--config", "/etc/veil/generated/caddy/Caddyfile"})
	if result.Valid || result.Error != "validation timed out" {
		t.Fatalf("expected timeout error, got %+v", result)
	}
}

func TestRunFixedConfigValidationReturnsTempDirError(t *testing.T) {
	// Make os.MkdirTemp fail by pointing TMPDIR at a file instead of a directory.
	tmp := t.TempDir()
	badDir := filepath.Join(tmp, "notadir")
	if err := writeFile(badDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	t.Setenv("TMPDIR", badDir)

	result := RunFixedConfigValidation("caddy", "/etc/veil/generated/caddy/Caddyfile", []string{"caddy", "validate", "--config", "/etc/veil/generated/caddy/Caddyfile"})
	if result.Valid || result.Error == "" {
		t.Fatalf("expected temp dir error, got %+v", result)
	}
}

func writeFile(path string, body []byte, mode int) error {
	return os.WriteFile(path, body, os.FileMode(mode))
}
