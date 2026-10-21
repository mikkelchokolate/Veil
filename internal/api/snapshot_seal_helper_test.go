package api

import (
	"bytes"
	"log"
	"os"
	"strconv"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

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

// simulatePreSealStateFile rewrites state.json as a pre-#1280 build left it
// (schemaVersion below SnapshotSealSchemaVersion) and re-points every row bound
// to the old bytes at the rewritten file, as that build would have written
// them. It returns the pre-seal file bytes.
func simulatePreSealStateFile(t *testing.T, state *managementState) []byte {
	t.Helper()
	body, err := os.ReadFile(state.statePath)
	if err != nil {
		t.Fatal(err)
	}
	current := []byte(`"schemaVersion": ` + strconv.Itoa(managementstate.CurrentSchemaVersion))
	legacy := []byte(`"schemaVersion": ` + strconv.Itoa(managementstate.SnapshotSealSchemaVersion-1))
	if !bytes.Contains(body, current) {
		t.Fatalf("state.json is not at schema %d: %s", managementstate.CurrentSchemaVersion, body)
	}
	rewritten := bytes.Replace(body, current, legacy, 1)
	if err := atomicfile.Write(state.statePath, rewritten, 0o600, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`UPDATE revision_snapshots SET state_sha256=? WHERE state_sha256=?`,
		managementstate.EncodedStateSHA256(rewritten), managementstate.EncodedStateSHA256(body)); err != nil {
		t.Fatal(err)
	}
	return rewritten
}

// captureStartupLog collects the standard logger's output for the rest of the
// test, so a fail-closed startup can be pinned to its cause.
func captureStartupLog(t *testing.T) *lockedLogBuffer {
	t.Helper()
	buffer := &lockedLogBuffer{}
	previous := log.Writer()
	log.SetOutput(buffer)
	t.Cleanup(func() { log.SetOutput(previous) })
	return buffer
}
