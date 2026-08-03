package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateCreatesSchemaMigrationsAndSetsVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	var version int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if version < 1 {
		t.Fatalf("expected at least one migration applied, got %d", version)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatalf("count migrations after: %v", err)
	}
	if before != after {
		t.Fatalf("idempotent migrate changed count: before=%d after=%d", before, after)
	}
}

func TestMigrationChecksumDetectsEditedHistory(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum='deadbeef' WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("edited historical migration was not rejected: %v", err)
	}
}

func TestMigrateCreatesDomainTables(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	for _, table := range []string{
		"revisions",
		"apply_jobs",
		"clients",
		"client_bindings",
		"client_credentials",
		"subscription_tokens",
		"traffic_counters",
		"traffic_runtime_state",
		"traffic_samples",
	} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("expected table %s: %v", table, err)
		}
	}
}

func TestOpenEnablesForeignKeysAndWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=ON, got %d", fk)
	}

	var journal string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", journal)
	}
}

func TestOpenHandlesURIReservedPathCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil ?#%.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open reserved path: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at exact path: %v", err)
	}
	db, err = OpenExisting(path)
	if err != nil {
		t.Fatalf("OpenExisting reserved path: %v", err)
	}
	_ = db.Close()
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "veil.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}
