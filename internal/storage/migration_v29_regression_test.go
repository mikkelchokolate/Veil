package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// openHistoryDB opens a scratch database with the production DSN (WAL +
// busy_timeout + foreign_keys) so replayed history matches how real
// databases were written.
func openHistoryDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteFileDSN(filepath.Join(t.TempDir(), name), false))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// applyHistory runs migrations[0:upto) raw and records them in
// schema_migrations, simulating a database at an older release.
func applyHistory(t *testing.T, db *sql.DB, upto int) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now')))`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:upto] {
		if _, err := db.Exec(migration.sql); err != nil {
			t.Fatalf("apply migration %d: %v", migration.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,name) VALUES(?,?)`, migration.version, migration.name); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMigration20NullableCreatedBy (#1139): created_by was nullable in the
// table migration 20 copies from, so `label=created_by` wrote NULL into a
// NOT NULL column and aborted on any row that carried NULL.
func TestMigration20NullableCreatedBy(t *testing.T) {
	db := openHistoryDB(t, "nullable-created-by.db")
	applyHistory(t, db, 20-1) // versions 1..19 applied; v20 pending

	if _, err := db.Exec(`INSERT INTO clients(id,name,enabled,created_at,updated_at,version,quota_reset_policy)
	  VALUES('c1','tok-owner',1,0,0,1,'never')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_tokens(id,client_id,token_hash,token_prefix,enabled,created_at,created_by)
	  VALUES('t1','c1',x'0000000000000000000000000000000000000000000000000000000000000001','pfx',1,0,NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate with NULL created_by: %v", err)
	}
	var label, createdBy string
	if err := db.QueryRow(`SELECT label, created_by FROM subscription_tokens WHERE id='t1'`).Scan(&label, &createdBy); err != nil {
		t.Fatal(err)
	}
	if label != "" || createdBy != "" {
		t.Fatalf("label=%q created_by=%q, want both empty", label, createdBy)
	}
}

// TestLegacyMigrationChecksumsAccepted (#1139): databases that already ran
// the superseded bodies of migrations 16/20/25 recorded their digests; the
// verifier must accept those checksums while still rejecting unknown edits.
func TestLegacyMigrationChecksumsAccepted(t *testing.T) {
	db := openHistoryDB(t, "legacy-checksum.db")
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL DEFAULT '',applied_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:25] {
		if _, err := db.Exec(migration.sql); err != nil {
			t.Fatalf("apply migration %d: %v", migration.version, err)
		}
		checksum := migrationChecksum(migration)
		if len(migration.legacySQL) > 0 {
			checksum = migrationChecksumText(migration.name, migration.legacySQL[0])
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,name,checksum) VALUES(?,?,?)`, migration.version, migration.name, checksum); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate over legacy-checksummed history: %v", err)
	}
	// And a checksum that matches neither body still fails closed.
	db2 := openHistoryDB(t, "tampered.db")
	applyHistory(t, db2, 1)
	if _, err := db2.Exec(`ALTER TABLE schema_migrations ADD COLUMN checksum TEXT NOT NULL DEFAULT 'deadbeef'`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db2); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered checksum: got %v, want checksum mismatch", err)
	}
}

// TestMigration29RuntimeIdentityNormalization (#1111/#1140): stored
// mixed-case identities fold to lowercase, over-length generated identities
// realign to the truncated form, the NOCASE unique index forbids case-only
// collisions, and the binding-cleanup trigger no longer uses LIKE so a
// restored binding id containing wildcards cannot over-delete state.
func TestMigration29RuntimeIdentityNormalization(t *testing.T) {
	db := openHistoryDB(t, "ri.db")
	applyHistory(t, db, 29-1)

	if _, err := db.Exec(`INSERT INTO clients(id,name,enabled,created_at,updated_at,version,quota_reset_policy)
	  VALUES('c1','owner',1,0,0,1,'never'),('c2','second',1,0,0,1,'never')`); err != nil {
		t.Fatal(err)
	}
	// Mixed-case identity (pre-validation row) + over-length v_ identity
	// (migration 7 derived the full non-dashed id without truncation; a
	// binding id longer than a UUID's 36 chars produces a >34-char identity
	// that GenerateRuntimeIdentity would truncate).
	if _, err := db.Exec(`INSERT INTO client_bindings(id,client_id,inbound_id,runtime_identity,enabled,created_at,updated_at,version)
	  VALUES('b1','c1','in-1','MiXeDCase',1,0,0,1)`); err != nil {
		t.Fatal(err)
	}
	longID := strings.Repeat("ab", 20) // 40 chars: derived identity = v_+40
	if _, err := db.Exec(`INSERT INTO client_bindings(id,client_id,inbound_id,runtime_identity,enabled,created_at,updated_at,version)
	  VALUES(?, 'c1','in-2',?,1,0,0,1)`, longID, "v_"+longID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var folded string
	if err := db.QueryRow(`SELECT runtime_identity FROM client_bindings WHERE id='b1'`).Scan(&folded); err != nil {
		t.Fatal(err)
	}
	if folded != "mixedcase" {
		t.Fatalf("identity %q, want folded lowercase", folded)
	}
	var realigned string
	if err := db.QueryRow(`SELECT runtime_identity FROM client_bindings WHERE id=?`, longID).Scan(&realigned); err != nil {
		t.Fatal(err)
	}
	if want := "v_" + longID[:32]; realigned != want {
		t.Fatalf("realigned identity %q, want %q", realigned, want)
	}
	// NOCASE uniqueness: an insert differing only by case must collide.
	if _, err := db.Exec(`INSERT INTO client_bindings(id,client_id,inbound_id,runtime_identity,enabled,created_at,updated_at,version)
	  VALUES('b2','c2','in-1','MIXEDCASE',1,0,0,1)`); err == nil {
		t.Fatal("case-only duplicate identity accepted")
	}
	// A different identity on a different inbound is still fine.
	if _, err := db.Exec(`INSERT INTO client_bindings(id,client_id,inbound_id,runtime_identity,enabled,created_at,updated_at,version)
	  VALUES('b3','c2','in-9','mixedcase',1,0,0,1)`); err != nil {
		t.Fatalf("cross-inbound same identity rejected: %v", err)
	}
	// Wildcard binding id: deleting 'b_' must remove its own runtime state
	// but NOT 'inst:bX' — under the old LIKE '%:b_' pattern the '_' wildcard
	// would have swept that unrelated provider row too.
	if _, err := db.Exec(`INSERT INTO traffic_runtime_state(provider_key,runtime_instance,last_upload_raw,last_download_raw,last_observed_at)
	  VALUES('inst:b_','inst',1,1,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO traffic_runtime_state(provider_key,runtime_instance,last_upload_raw,last_download_raw,last_observed_at)
	  VALUES('inst:bX','inst',1,1,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO client_bindings(id,client_id,inbound_id,runtime_identity,enabled,created_at,updated_at,version)
	  VALUES('b_','c2','in-3','wildidentity',1,0,0,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM client_bindings WHERE id='b_'`); err != nil {
		t.Fatal(err)
	}
	var own, kept int
	if err := db.QueryRow(`SELECT COUNT(*) FROM traffic_runtime_state WHERE provider_key='inst:b_'`).Scan(&own); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM traffic_runtime_state WHERE provider_key='inst:bX'`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if own != 0 {
		t.Fatalf("own runtime row not cleaned: %d remain", own)
	}
	if kept != 1 {
		t.Fatalf("LIKE wildcard over-deleted unrelated runtime row")
	}
}
