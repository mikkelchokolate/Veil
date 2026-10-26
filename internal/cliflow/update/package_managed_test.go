package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestDetectPackageManagedBinaryUsesMarker locks the primary #1424 signal: the
// package's postinstall writes a marker next to the installed environment.
func TestDetectPackageManagedBinaryUsesMarker(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, []byte("the Veil OS package\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalMarkers := packageManagedMarkers
	packageManagedMarkers = []string{marker}
	t.Cleanup(func() { packageManagedMarkers = originalMarkers })

	owner, managed := DetectPackageManagedBinary("/usr/local/bin/veil")
	if !managed || owner != "the Veil OS package" {
		t.Fatalf("owner=%q managed=%v, want the marker owner", owner, managed)
	}
}

// TestDetectPackageManagedBinaryFallsBackToPackageDatabases covers hosts
// installed by a package built before the marker existed.
func TestDetectPackageManagedBinaryFallsBackToPackageDatabases(t *testing.T) {
	originalMarkers, originalRunner := packageManagedMarkers, packageQueryRunner
	packageManagedMarkers = []string{filepath.Join(t.TempDir(), "absent")}
	packageQueryRunner = func(name string, args ...string) ([]byte, error) {
		if name == "dpkg-query" {
			return []byte("veil: /usr/local/bin/veil\n"), nil
		}
		return nil, errors.New("not installed")
	}
	t.Cleanup(func() {
		packageManagedMarkers = originalMarkers
		packageQueryRunner = originalRunner
	})

	owner, managed := DetectPackageManagedBinary("/usr/local/bin/veil")
	if !managed || owner != "veil: /usr/local/bin/veil" {
		t.Fatalf("owner=%q managed=%v, want the dpkg owner", owner, managed)
	}
}

// TestDetectPackageManagedBinaryAllowsAnUnmanagedBinary keeps the operator
// tarball/source install working when no package owns the path.
func TestDetectPackageManagedBinaryAllowsAnUnmanagedBinary(t *testing.T) {
	originalMarkers, originalRunner := packageManagedMarkers, packageQueryRunner
	packageManagedMarkers = []string{filepath.Join(t.TempDir(), "absent")}
	packageQueryRunner = func(string, ...string) ([]byte, error) { return nil, errors.New("exit status 1") }
	t.Cleanup(func() {
		packageManagedMarkers = originalMarkers
		packageQueryRunner = originalRunner
	})

	if owner, managed := DetectPackageManagedBinary("/usr/local/bin/veil"); managed {
		t.Fatalf("unmanaged binary reported as package-owned by %q", owner)
	}
}
