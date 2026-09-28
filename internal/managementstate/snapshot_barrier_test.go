package managementstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseSnapshotBarrierReportsCloseFailure(t *testing.T) {
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "barrier"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshotBarrierLock(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := releaseSnapshotBarrier(file); err != nil {
		t.Fatalf("release snapshot barrier: %v", err)
	}
	if err := releaseSnapshotBarrier(file); err == nil {
		t.Fatal("expected releasing a closed barrier file to fail")
	}
}
