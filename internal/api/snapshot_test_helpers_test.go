package api

import "testing"

func mustStateSnapshot(t *testing.T, state *managementState) managementSnapshot {
	t.Helper()
	snapshot, err := state.snapshotLocked()
	if err != nil {
		t.Fatalf("snapshot management state: %v", err)
	}
	return snapshot
}
