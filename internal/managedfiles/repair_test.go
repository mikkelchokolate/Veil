package managedfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetPlansMissingAndDriftedFiles(t *testing.T) {
	dir := t.TempDir()
	matching := filepath.Join(dir, "matching.txt")
	drifted := filepath.Join(dir, "drifted.txt")
	missing := filepath.Join(dir, "missing.txt")
	if err := os.WriteFile(matching, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(drifted, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := NewSet([]File{
		{Path: matching, Content: "same", Mode: 0o600},
		{Path: drifted, Content: "new", Mode: 0o600},
		{Path: missing, Content: "created", Mode: 0o600},
	}).Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("actions = %+v", plan.Actions)
	}
	if plan.Actions[0].Path != drifted || plan.Actions[0].Reason != RepairReasonDrifted {
		t.Fatalf("first action = %+v", plan.Actions[0])
	}
	if plan.Actions[1].Path != missing || plan.Actions[1].Reason != RepairReasonMissing {
		t.Fatalf("second action = %+v", plan.Actions[1])
	}
	if !strings.Contains(plan.Summary(), "repair drifted") || !plan.HasChanges() {
		t.Fatalf("summary/changes mismatch: %q", plan.Summary())
	}
}

func TestApplyWritesPlannedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.txt")
	result, err := Apply(RepairPlan{Actions: []RepairAction{{Path: path, Reason: RepairReasonMissing, Content: "content", Mode: 0o600}}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "content" || len(result.WrittenFiles) != 1 || result.WrittenFiles[0] != path {
		t.Fatalf("body=%q result=%+v", body, result)
	}
}
