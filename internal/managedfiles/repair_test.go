package managedfiles

import (
	"os"
	"path/filepath"
	"runtime"
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
	if !plan.HasChanges() {
		t.Fatal("plan with actions must report changes")
	}
	// Exact summary format — one "repair <reason> <slash-path>" line each, in
	// plan order.
	want := "repair drifted " + filepath.ToSlash(drifted) + "\n" +
		"repair missing " + filepath.ToSlash(missing) + "\n"
	if got := plan.Summary(); got != want {
		t.Fatalf("Summary() = %q, want %q", got, want)
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
	// The managed file lands with exactly the planned mode and the created
	// parent directory carries the restrictive 0750 contract (audit #519).
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("managed file mode = %o, want 0600", info.Mode().Perm())
		}
		parent, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if parent.Mode().Perm() != 0o750 {
			t.Fatalf("managed parent dir mode = %o, want 0750", parent.Mode().Perm())
		}
	}
}
