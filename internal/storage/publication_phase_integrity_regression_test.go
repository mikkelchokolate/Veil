package storage

import (
	"strings"
	"testing"
)

// Regression for #1037: the runtime_publications validation triggers have
// always admitted phase='publishing' and recovery code still resolves it, but
// the startup domain-integrity check omitted it — so any database carrying a
// legacy 'publishing' receipt bricked storage.Open forever.
func TestMigrateAcceptsLegacyPublishingPublicationPhase(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO apply_jobs(id,desired_revision,base_revision,status,trigger,created_at)
VALUES('job-publishing',1,0,'applying','test',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id,revision,generation,snapshot_sha256,operations_json,published_at,phase)
VALUES('job-publishing',1,1,?,'[]',1,'publishing')`, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("legacy 'publishing' receipt rejected by domain integrity: %v", err)
	}
}

// Companion guard: phases outside the trigger allow-list are still refused,
// so admitting 'publishing' did not open the gate to arbitrary phases.
func TestMigrateRejectsUnknownPublicationPhase(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO apply_jobs(id,desired_revision,base_revision,status,trigger,created_at)
VALUES('job-bogus',1,0,'applying','test',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_publications(job_id,revision,generation,snapshot_sha256,operations_json,published_at,phase)
VALUES('job-bogus',1,1,?,'[]',1,'bogus-phase')`, strings.Repeat("d", 64)); err == nil {
		t.Fatal("unknown publication phase was accepted by the schema trigger")
	}
}
