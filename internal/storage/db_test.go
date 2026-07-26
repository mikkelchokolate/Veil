package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenExistingRequiresDatabaseAndDoesNotCreateIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil.db")
	if _, err := OpenExisting(path); err == nil {
		t.Fatal("OpenExisting created a missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing database was created: %v", err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	existing, err := OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer existing.Close()
	var version int
	if err := existing.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version == 0 {
		t.Fatal("existing database schema was not readable")
	}
}
