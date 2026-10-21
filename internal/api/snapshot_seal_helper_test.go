package api

import "testing"

// resealSnapshotForTest re-authenticates a revision_snapshots row a test has
// rewritten on purpose (fixture corruption or a legacy payload shape), so the
// test keeps exercising the reader behind the #1280 MAC rather than the MAC.
func resealSnapshotForTest(t *testing.T, state *managementState, revision uint64) {
	t.Helper()
	seal := state.applySnapshots.Seal()
	if seal == nil {
		t.Fatal("snapshot store is not armed")
	}
	var payload, digest string
	var createdAt int64
	if err := state.db.QueryRow(`SELECT payload,state_sha256,created_at FROM revision_snapshots WHERE revision=?`, revision).
		Scan(&payload, &digest, &createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`UPDATE revision_snapshots SET payload_mac=? WHERE revision=?`,
		seal.MAC(revision, createdAt, digest, []byte(payload)), revision); err != nil {
		t.Fatal(err)
	}
}
