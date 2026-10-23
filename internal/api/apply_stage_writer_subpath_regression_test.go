package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteApplyStageRejectsRenderedPathsOutsideGeneratedRoot (#1273 item 3):
// the stage writer used to write every renderer-supplied path verbatim, so a
// renderer emitting a `..` or absolute subpath could write anywhere the veil
// uid can. Every rendered path must now be a canonical path strictly beneath
// <applyRoot>/generated and outside the writer's own generated/veil
// directory, and a rejected set must not write any rendered file.
func TestWriteApplyStageRejectsRenderedPathsOutsideGeneratedRoot(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(root string) string
	}{
		{"dotdot-escape", func(root string) string {
			return filepath.Join(root, "generated", "caddy") + string(filepath.Separator) + filepath.Join("..", "..", "escaped.json")
		}},
		{"cleaned-escape", func(root string) string { return filepath.Join(root, "escaped.json") }},
		{"sibling-prefix", func(root string) string { return filepath.Join(root, "generated-evil", "x.json") }},
		{"outside-root", func(root string) string { return filepath.Join(filepath.Dir(root), "outside.json") }},
		{"generated-root-itself", func(root string) string { return filepath.Join(root, "generated") }},
		{"non-canonical-inside", func(root string) string {
			return filepath.Join(root, "generated", "caddy") + string(filepath.Separator) + "." + string(filepath.Separator) + "config.json"
		}},
		{"stage-state-file", func(root string) string {
			return filepath.Join(root, "generated", "veil", "management-state.json")
		}},
		{"stage-veil-dir", func(root string) string { return filepath.Join(root, "generated", "veil", "other.json") }},
		{"relative", func(string) string { return filepath.Join("generated", "caddy", "config.json") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			// A relative rendered path resolves against the working directory;
			// keep any write a regression might make inside the temp dir.
			t.Chdir(parent)
			root := filepath.Join(parent, "apply")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			good := filepath.Join(root, "generated", "caddy", "config.json")
			bad := tc.path(root)
			_, _, _, err := WriteApplyStage(ApplyStageInput{
				ApplyRoot: root,
				Plan:      ApplyPlanResponse{Valid: true},
				Snapshot:  managementSnapshot{Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}},
				Rendered:  map[string]string{good: "good", bad: "bad"},
				Validate: func([]string) []ConfigValidationResult {
					t.Fatal("validation ran for a rejected rendered set")
					return nil
				},
			})
			if err == nil || !strings.Contains(err.Error(), "rendered config path") {
				t.Fatalf("WriteApplyStage(%q) err = %v, want rendered config path rejection", bad, err)
			}
			if _, statErr := os.Stat(good); !os.IsNotExist(statErr) {
				t.Errorf("rejected set still wrote %s (stat err %v)", good, statErr)
			}
			if body, readErr := os.ReadFile(filepath.Clean(bad)); readErr == nil && string(body) == "bad" {
				t.Errorf("rejected path %s was written", bad)
			}
		})
	}
}

// TestWriteApplyStageAcceptsCanonicalGeneratedPaths pins the positive side:
// nested artifacts and routing-dat siblings under generated/ still stage.
func TestWriteApplyStageAcceptsCanonicalGeneratedPaths(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "generated", "caddy", "config.json"),
		filepath.Join(root, "generated", "hysteria2", "edge.yaml"),
		filepath.Join(root, "generated", "warp.json"),
		filepath.Join(root, "generated", "veilish", "x.json"),
	}
	rendered := map[string]string{}
	for _, path := range paths {
		rendered[path] = "body"
	}
	_, _, renderedPaths, err := WriteApplyStage(ApplyStageInput{
		ApplyRoot: root,
		Plan:      ApplyPlanResponse{Valid: true},
		Snapshot:  managementSnapshot{Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}},
		Rendered:  rendered,
		Validate:  func([]string) []ConfigValidationResult { return nil },
	})
	if err != nil {
		t.Fatalf("WriteApplyStage: %v", err)
	}
	if len(renderedPaths) != len(paths) {
		t.Fatalf("rendered paths = %v, want %d", renderedPaths, len(paths))
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected staged %s: %v", path, err)
		}
	}
}
