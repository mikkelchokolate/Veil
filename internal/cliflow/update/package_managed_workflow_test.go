package update

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunWorkflowRefusesAPackageManagedBinary locks #1424: the updater replaces
// only the executable, so on a host whose binary belongs to an OS package it
// must refuse and point at the package manager instead of leaving the old
// package's units and install hooks behind.
func TestRunWorkflowRefusesAPackageManagedBinary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dry   bool
		force bool
	}{
		{"apply", false, false},
		{"force does not bypass", false, true},
		{"dry run reports it too", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, _ := newValidWorkflowDeps(t)
			deps.PackageManaged = func(string) (string, bool) { return "veil 0.7.2 (dpkg)", true }
			replaced := false
			deps.ReplaceBinaryFromArchive = func(string, []byte, bool) (string, error) {
				replaced = true
				return "", nil
			}
			var out bytes.Buffer
			err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true, DryRun: tc.dry, Force: tc.force}, &out, deps)
			if err == nil {
				t.Fatalf("update accepted a package-managed binary:\n%s", out.String())
			}
			for _, want := range []string{"belongs to the veil 0.7.2 (dpkg) package", "package manager"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %v does not mention %q", err, want)
				}
			}
			if replaced {
				t.Fatal("the updater replaced the binary before refusing")
			}
		})
	}
}

// TestRunWorkflowUpdatesAnUnmanagedBinary keeps the normal path working.
func TestRunWorkflowUpdatesAnUnmanagedBinary(t *testing.T) {
	deps, _ := newValidWorkflowDeps(t)
	deps.PackageManaged = func(string) (string, bool) { return "", false }
	replaced := false
	deps.ReplaceBinaryFromArchive = func(string, []byte, bool) (string, error) {
		replaced = true
		return "/tmp/veil.backup", nil
	}
	var out bytes.Buffer
	if err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true}, &out, deps); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !replaced {
		t.Fatal("unmanaged binary was not replaced")
	}
}
