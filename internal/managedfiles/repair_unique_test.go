package managedfiles

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCollapsesIdenticalDuplicatePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.env")
	owner := &FileOwner{UID: 0, GID: 0}
	files := []File{
		{Path: path, Content: "x", Mode: 0o600, Owner: owner},
		{Path: filepath.Join(dir, ".", "a.env"), Content: "x", Mode: 0o600, Owner: &FileOwner{UID: 0, GID: 0}},
	}
	plan, err := NewSet(files).Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Reason != RepairReasonMissing {
		t.Fatalf("actions = %+v, want one missing action", plan.Actions)
	}
}

func TestPlanRejectsConflictingDuplicatePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.env")
	base := File{Path: path, Content: "x", Mode: 0o600}
	cases := map[string]File{
		"content":  {Path: path, Content: "y", Mode: 0o600},
		"mode":     {Path: path, Content: "x", Mode: 0o640},
		"dir mode": {Path: path, Content: "x", Mode: 0o600, DirMode: 0o755},
		"owner":    {Path: path, Content: "x", Mode: 0o600, Owner: &FileOwner{UID: 0, GID: 1}},
	}
	for name, dup := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewSet([]File{base, dup}).Plan()
			if err == nil || !strings.Contains(err.Error(), "listed twice") {
				t.Fatalf("Plan error = %v, want conflicting duplicate refusal", err)
			}
		})
	}
	t.Run("owner value", func(t *testing.T) {
		a := File{Path: path, Content: "x", Mode: 0o600, Owner: &FileOwner{UID: 0, GID: 0}}
		b := File{Path: path, Content: "x", Mode: 0o600, Owner: &FileOwner{UID: 0, GID: 1}}
		if _, err := NewSet([]File{a, b}).Plan(); err == nil {
			t.Fatal("owners with different gid must conflict")
		}
	})
}
