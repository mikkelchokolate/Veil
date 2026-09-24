package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationHistoryIteratorErrorFailsMigrate proves Migrate surfaces a
// history-iteration failure instead of reporting success (issue #872). The
// schema_migrations view explodes on the second row, so rows.Err() — not
// rows.Scan — is what must carry the failure.
func TestMigrationHistoryIteratorErrorFailsMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE migration_history(version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// One valid row, then a row whose name expression fails to evaluate so the
	// history SELECT errors during iteration rather than on the first row.
	if _, err := db.Exec(`INSERT INTO migration_history(version,name,checksum) VALUES(?,?,?)`,
		migrations[0].version, migrations[0].name, migrationChecksum(migrations[0])); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO migration_history(version,name,checksum) VALUES(2,'unreadable','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE VIEW schema_migrations AS
SELECT version,
       CASE WHEN version>1 THEN json_extract(name,'$') ELSE name END AS name,
       checksum
FROM migration_history`); err != nil {
		t.Fatal(err)
	}

	err = Migrate(db)
	if err == nil {
		t.Fatal("Migrate succeeded despite a mid-iteration history failure")
	}
	if !strings.Contains(err.Error(), "iterate migration history") {
		t.Fatalf("Migrate error = %v, want the iterator failure to be reported", err)
	}
}
