package storage

import (
	"database/sql"
	"testing"
)

// TestMigration30NullsHistoricalZeroConnectionLimits (#1212): the pre-v30
// trigger admitted device_limit=0 (only <0 aborted), so a database could
// carry rows that the tightened triggers AND the every-Open integrity scan
// reject — migrating fine and then wedging Open. The migration must NULL
// the historical zeros before the new guards exist.
func TestMigration30NullsHistoricalZeroConnectionLimits(t *testing.T) {
	db := openHistoryDB(t, "v30-zero-limits.db")
	applyHistory(t, db, 30-1) // versions 1..29 applied; v30 pending

	// The v29-era trigger allows device_limit=0 — it is exactly the row
	// shape that used to wedge Open after migrating.
	if _, err := db.Exec(`INSERT INTO clients(id,name,enabled,created_at,updated_at,version,quota_reset_policy,device_limit)
	  VALUES('c1','legacy-zero',1,0,0,1,'never',0)`); err != nil {
		t.Fatalf("pre-v30 trigger must still admit device_limit=0: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clients(id,name,enabled,created_at,updated_at,version,quota_reset_policy,device_limit)
	  VALUES('c2','positive',1,0,0,1,'never',3)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate over historical device_limit=0 rows: %v", err)
	}
	var deviceLimit sql.NullInt64
	if err := db.QueryRow(`SELECT device_limit FROM clients WHERE id='c1'`).Scan(&deviceLimit); err != nil {
		t.Fatal(err)
	}
	if deviceLimit.Valid {
		t.Fatalf("device_limit = %v, want NULL after migration", deviceLimit.Int64)
	}
	if err := db.QueryRow(`SELECT device_limit FROM clients WHERE id='c2'`).Scan(&deviceLimit); err != nil {
		t.Fatal(err)
	}
	if !deviceLimit.Valid || deviceLimit.Int64 != 3 {
		t.Fatalf("positive device_limit must be preserved, got %+v", deviceLimit)
	}
	// The tightened guards reject new zeros on either connection-limit
	// column while still admitting positive values and NULL.
	if _, err := db.Exec(`INSERT INTO clients(id,name,enabled,created_at,updated_at,version,quota_reset_policy,device_limit,ip_limit)
	  VALUES('c3','new-zero',1,0,0,1,'never',0,0)`); err == nil {
		t.Fatal("device_limit=0 insert must be rejected by the tightened trigger")
	}
	if _, err := db.Exec(`INSERT INTO clients(id,name,enabled,created_at,updated_at,version,quota_reset_policy,device_limit,ip_limit)
	  VALUES('c4','new-ok',1,0,0,1,'never',2,NULL)`); err != nil {
		t.Fatalf("positive device_limit / NULL ip_limit insert rejected: %v", err)
	}
	// The integrity scan runs inside Migrate, so reaching this point already
	// proves the wedge is gone for the migrating database.
}

// TestMigration30LegacyChecksumAccepted (#1212): databases that applied the
// superseded v30 body recorded its checksum — the verifier must accept that
// digest (legacySQL) instead of declaring the history tampered.
func TestMigration30LegacyChecksumAccepted(t *testing.T) {
	db := openHistoryDB(t, "v30-legacy.db")
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL DEFAULT '',applied_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:29] {
		if _, err := db.Exec(migration.sql); err != nil {
			t.Fatalf("apply migration %d: %v", migration.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,name,checksum) VALUES(?,?,?)`, migration.version, migration.name, migrationChecksum(migration)); err != nil {
			t.Fatal(err)
		}
	}
	v30 := migrations[29]
	if v30.version != 30 || len(v30.legacySQL) != 1 {
		t.Fatalf("migration 30 must carry exactly one legacy body, got %+v", v30)
	}
	if _, err := db.Exec(v30.legacySQL[0]); err != nil {
		t.Fatalf("apply legacy v30 body: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version,name,checksum) VALUES(?,?,?)`,
		v30.version, v30.name, migrationChecksumText(v30.name, v30.legacySQL[0])); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("legacy v30 checksum must be accepted: %v", err)
	}
}
