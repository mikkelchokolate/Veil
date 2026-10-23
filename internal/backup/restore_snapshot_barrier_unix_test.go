//go:build unix

package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestRestorePublishWaitsForCrossProcessSnapshotBarrier covers #1123: the
// capture→publish window (previous-digest capture through member replacement)
// must hold .veil-snapshot.lock — the same flock backup create takes — so a
// racing management-state commit from another process cannot slip between the
// safety digest capture and the safety rename.
func TestRestorePublishWaitsForCrossProcessSnapshotBarrier(t *testing.T) {
	fixture := prepareRestoreTripleFixture(t)
	lockPath := filepath.Join(filepath.Dir(fixture.statePath), ".veil-snapshot.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := RestoreBackupFileWithOptions(fixture.archive, fixture.statePath, fixture.keyPath, "",
			RestoreOptions{DatabasePath: fixture.databasePath, AllowUnencrypted: true})
		done <- err
	}()

	// While a foreign process holds the barrier the restore must not reach
	// publish — no journal, no safety rename, no replacement.
	select {
	case err := <-done:
		t.Fatalf("restore completed while another holder owned the snapshot barrier: %v", err)
	case <-time.After(750 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.statePath), restoreTransactionJournalName)); !os.IsNotExist(err) {
		t.Fatal("restore staged a journal while the snapshot barrier was held")
	}

	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := lockFile.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("restore after barrier release: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("restore did not finish after the snapshot barrier was released")
	}
	state, err := os.ReadFile(fixture.statePath)
	if err != nil || !bytes.Equal(state, fixture.intendedState) {
		t.Fatal("restore did not publish the intended state.json")
	}
}
