package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationDomainIntegrityCompletionRejectsInvalidRows(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO apply_jobs(id,desired_revision,base_revision,status,trigger,created_at) VALUES('bad',1,0,'mystery','test',1)`); err == nil {
		t.Fatal("expected invalid apply status rejection")
	}
	if _, err := db.Exec(`INSERT INTO traffic_samples(bucket_start,client_id,binding_id,upload_delta,download_delta) VALUES(1,'missing','',1,1)`); err == nil {
		t.Fatal("expected orphan traffic sample rejection")
	}
}

func TestMigrateRejectsPreexistingInvalidDomainRows(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "invalid-domain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now')))`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:8] {
		if _, err := db.Exec(migration.sql); err != nil {
			t.Fatalf("apply migration %d: %v", migration.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,name) VALUES(?,?)`, migration.version, migration.name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO apply_jobs(id,desired_revision,base_revision,status,trigger,created_at) VALUES('bad',1,0,'mystery','test',1)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err == nil || !strings.Contains(err.Error(), "invalid apply jobs") {
		t.Fatalf("expected invalid pre-existing domain rejection, got %v", err)
	}
}

func TestMigrateRejectsNonCanonicalHistory(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		version       int
		migrationName string
	}{
		{"gap", 2, migrations[1].name},
		{"renamed", 1, "wrong-name"},
		{"unknown", 999, "future"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "db.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO schema_migrations(version,name) VALUES(?,?)`, fixture.version, fixture.migrationName); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(db); err == nil {
				t.Fatal("accepted non-canonical migration history")
			}
		})
	}
}

func TestMigrationDomainGuardsRejectInvalidValues(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO clients(id,name,enabled,quota_reset_policy,notes,depleted,created_at,updated_at,version) VALUES('bad','bad',2,'never','',0,1,1,1)`)
	if err == nil {
		t.Fatal("invalid boolean domain value was accepted")
	}
	if _, err := db.Exec(`INSERT INTO revisions(id,desired_revision,applied_revision) VALUES(1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE revisions SET desired_revision=0, applied_revision=1 WHERE id=1`); err == nil {
		t.Fatal("invalid revision ordering was accepted")
	}
	if _, err := db.Exec(`INSERT INTO traffic_counters(client_id,binding_id,upload_bytes,download_bytes,updated_at) VALUES('x','',-1,0,1)`); err == nil {
		t.Fatal("negative traffic counter was accepted")
	}
}

func TestMigrateRunsForeignKeyCheck(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO client_bindings(id,client_id,inbound_id,enabled,protocol_settings,created_at,updated_at,version,runtime_identity) VALUES('binding','missing','inbound',1,'{}',1,1,1,'identity')`)
	if err != nil {
		t.Fatalf("seed FK violation: %v", err)
	}
	if err := Migrate(db); err == nil {
		t.Fatal("foreign-key violation was not reported")
	} else if got := fmt.Sprint(err); got == "" {
		t.Fatal("empty migration error")
	}
}
