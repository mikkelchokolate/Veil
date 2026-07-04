package managedfiles

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPlanReturnsReadError(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "isdir")
	if err := os.Mkdir(badPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := NewSet([]File{{Path: badPath, Content: "x", Mode: 0o600}}).Plan()
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
}

func TestPlanTreatsENOTDIRAsMissing(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "parent")
	if err := os.WriteFile(parentFile, []byte("parent"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parentFile, "child.txt")

	plan, err := NewSet([]File{{Path: path, Content: "expected", Mode: 0o600}}).Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("actions = %+v", plan.Actions)
	}
	if plan.Actions[0].Reason != RepairReasonMissing {
		t.Fatalf("reason = %q, want %q", plan.Actions[0].Reason, RepairReasonMissing)
	}
}

func TestSummaryNoActions(t *testing.T) {
	plan := RepairPlan{}
	if plan.HasChanges() {
		t.Fatal("HasChanges should be false for empty plan")
	}
	want := "No repair actions required\n"
	if got := plan.Summary(); got != want {
		t.Fatalf("Summary() = %q, want %q", got, want)
	}
}

func TestApplyReturnsWriteError(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "parent")
	if err := os.WriteFile(parentFile, []byte("parent"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parentFile, "child.txt")

	_, err := Apply(RepairPlan{Actions: []RepairAction{{Path: path, Content: "x", Mode: 0o600}}})
	if err == nil {
		t.Fatal("expected error when parent path is not a directory")
	}
}

func TestWriteFileMkdirAllError(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "parent")
	if err := os.WriteFile(parentFile, []byte("parent"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteFile(filepath.Join(parentFile, "child.txt"), "x", 0o600)
	if err == nil {
		t.Fatal("expected error from MkdirAll when parent is a file")
	}
}

func TestWriteFileWriteError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the temporary path (target + ".tmp") an existing directory so
	// os.WriteFile fails because it cannot open a directory for writing.
	if err := os.Mkdir(target+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}

	err := WriteFile(target, "x", 0o600)
	if err == nil {
		t.Fatal("expected error writing when temporary path is a directory")
	}
}

func TestIsMissingOrBlocked(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not exist",
			err:  &os.PathError{Op: "open", Path: "/missing", Err: os.ErrNotExist},
			want: true,
		},
		{
			name: "ENOTDIR",
			err:  &os.PathError{Op: "open", Path: "/x/y", Err: syscall.ENOTDIR},
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMissingOrBlocked(tc.err); got != tc.want {
				t.Fatalf("IsMissingOrBlocked(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
