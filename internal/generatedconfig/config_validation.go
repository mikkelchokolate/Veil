package generatedconfig

import (
	"os"
	"path/filepath"
)

type ConfigValidationRunner func(name string, config string, command []string) ConfigValidationResult

type runtimeCommandRunner interface {
	Run(RuntimeCommandInput) RuntimeCommandOutput
}

// runtimeCommandExecutor is swapped in tests to avoid invoking real binaries.
var runtimeCommandExecutor runtimeCommandRunner = NewRuntimeCommandExecutor()

type StagedConfigValidator struct {
	run     ConfigValidationRunner
	catalog ArtifactCatalog
}

// NewStagedConfigValidator creates a validator using the legacy fixed artifact
// catalog. Callers that can reach the protocol registry should use
// NewStagedConfigValidatorWithCatalog with NewArtifactCatalogFromRegistry so
// validation follows registered protocol plugins.
func NewStagedConfigValidator(run ConfigValidationRunner) StagedConfigValidator {
	return NewStagedConfigValidatorWithCatalog(run, NewDefaultArtifactCatalog())
}

// NewStagedConfigValidatorWithCatalog creates a validator backed by the given
// artifact catalog. Passing a registry-built catalog keeps validation in sync
// with the installed set of protocol plugins.
func NewStagedConfigValidatorWithCatalog(run ConfigValidationRunner, catalog ArtifactCatalog) StagedConfigValidator {
	if run == nil {
		run = RunFixedConfigValidation
	}
	return StagedConfigValidator{run: run, catalog: catalog}
}

func (v StagedConfigValidator) Validate(paths []string) []ConfigValidationResult {
	results := []ConfigValidationResult{}
	catalog := NewConfigValidationCatalog(v.catalog)
	for _, path := range paths {
		validation, ok := catalog.Match(path)
		if ok {
			results = append(results, v.run(validation.Name, validation.Config, validation.Command))
		}
	}
	return results
}

func RunFixedConfigValidation(name string, config string, command []string) ConfigValidationResult {
	result := ConfigValidationResult{Name: name, Config: config, Command: append([]string(nil), command...)}
	// Caddy's internal CA needs a writable home directory; veil's locked user
	// has HOME=/nonexistent. Use a fresh temp directory so validation does not
	// collide with root-owned production Caddy storage under /var/lib/caddy.
	tmpDir, err := os.MkdirTemp("", "veil-caddy-validate-*")
	if err != nil {
		result.Error = "create validation temp dir: " + err.Error()
		return result
	}
	defer os.RemoveAll(tmpDir)
	env := append(os.Environ(),
		"HOME="+tmpDir,
		"XDG_DATA_HOME="+filepath.Join(tmpDir, "caddy"),
		"XDG_CONFIG_HOME="+tmpDir,
	)
	output := runtimeCommandExecutor.Run(RuntimeCommandInput{Command: command, Env: env})
	result.Output = output.Output
	if output.Empty {
		result.Skipped = true
		result.Error = "validator command is empty"
		return result
	}
	if output.NotFound {
		result.Skipped = true
		result.Error = command[0] + " not found; syntax validation skipped"
		return result
	}
	if output.TimedOut {
		result.Error = "validation timed out"
		return result
	}
	if output.Err != nil {
		result.Error = output.Err.Error()
		return result
	}
	result.Valid = true
	return result
}
