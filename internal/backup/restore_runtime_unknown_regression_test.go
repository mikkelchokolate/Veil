package backup

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestRestoredDatabaseHistoricalAppliedRevisionBecomesRuntimeUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restored.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO revisions(id,desired_revision,applied_revision) VALUES(1,7,7)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO revision_snapshots(revision,payload,state_sha256) VALUES(7,'{}',?)`, string(make([]byte, 64))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE apply_lease SET owner_process='old',current_operation='old-op',generation=4,heartbeat_at=1,lease_expires_at=9999999999 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := prepareRestoredDatabaseRuntimeUnknown(path, 9); err != nil {
		t.Fatal(err)
	}
	db, err = storage.OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var desired, applied, historical, verified, generation uint64
	var status, owner, operation string
	if err := db.QueryRow(`SELECT desired_revision,applied_revision FROM revisions WHERE id=1`).Scan(&desired, &applied); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT historical_applied_revision,verified_revision,status FROM runtime_verification WHERE id=1`).Scan(&historical, &verified, &status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT generation,owner_process,current_operation FROM apply_lease WHERE id=1`).Scan(&generation, &owner, &operation); err != nil {
		t.Fatal(err)
	}
	if desired != 7 || applied != 0 || historical != 7 || verified != 0 || status != "unknown" {
		t.Fatalf("restored runtime verification state = desired:%d applied:%d historical:%d verified:%d status:%s", desired, applied, historical, verified, status)
	}
	if generation < 9 || owner != "" || operation != "" {
		t.Fatalf("restored fencing floor not neutralized: generation=%d owner=%q operation=%q", generation, owner, operation)
	}
}
