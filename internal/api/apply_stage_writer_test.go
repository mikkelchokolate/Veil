package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanStaleGeneratedFilesRemovesOnlyManagedStaleFiles(t *testing.T) {
	root := t.TempDir()
	caddyDir := filepath.Join(root, "generated", "caddy")
	mieruDir := filepath.Join(root, "generated", "mieru")
	otherDir := filepath.Join(root, "generated", "other")
	for _, dir := range []string{caddyDir, mieruDir, otherDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	staleCaddy := filepath.Join(caddyDir, "stale.json")
	keepCaddy := filepath.Join(caddyDir, "config.json")
	staleMieru := filepath.Join(mieruDir, "stale.json")
	otherFile := filepath.Join(otherDir, "preserve.me")
	for _, path := range []string{staleCaddy, keepCaddy, staleMieru, otherFile} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	keep := map[string]bool{keepCaddy: true}
	if err := cleanStaleGeneratedFiles(filepath.Join(root, "generated"), keep); err != nil {
		t.Fatalf("cleanStaleGeneratedFiles: %v", err)
	}

	if _, err := os.Stat(staleCaddy); !os.IsNotExist(err) {
		t.Errorf("stale caddy file should be removed: %v", err)
	}
	if _, err := os.Stat(staleMieru); !os.IsNotExist(err) {
		t.Errorf("stale mieru file should be removed: %v", err)
	}
	if _, err := os.Stat(keepCaddy); err != nil {
		t.Errorf("kept caddy file should remain: %v", err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Errorf("unmanaged file outside managed subdirs should be preserved: %v", err)
	}
}

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
