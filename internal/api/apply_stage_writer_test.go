package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteApplyStageWritesPlanSnapshotAndRenderedConfigs(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "generated", "caddy", "Caddyfile")
	written, validations, renderedPaths, err := WriteApplyStage(ApplyStageInput{
		ApplyRoot: root,
		Plan:      ApplyPlanResponse{Valid: true, Configs: []string{"caddy"}},
		Snapshot:  managementSnapshot{Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}},
		Rendered:  map[string]string{configPath: "caddy config"},
		Validate: func(paths []string) []ConfigValidationResult {
			if len(paths) != 1 || paths[0] != configPath {
				t.Fatalf("validation paths = %+v", paths)
			}
			return []ConfigValidationResult{{Name: "caddy", Valid: true}}
		},
	})
	if err != nil {
		t.Fatalf("WriteApplyStage: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "generated", "veil", "apply-plan.json"),
		filepath.Join(root, "generated", "veil", "management-state.json"),
		configPath,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected written file %s: %v", path, err)
		}
	}
	if len(written) != 3 || len(renderedPaths) != 1 || len(validations) != 1 {
		t.Fatalf("unexpected result: written=%+v rendered=%+v validations=%+v", written, renderedPaths, validations)
	}
}
