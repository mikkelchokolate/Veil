package testdb

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestTemplateIsClosedAndHasNoActiveWAL(t *testing.T) {
	fingerprint, activeWAL, err := TemplateInfo()
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" {
		t.Fatal("empty schema fingerprint")
	}
	if activeWAL {
		t.Fatal("template reports active WAL")
	}
}

func TestClonesAreWritableAndIsolated(t *testing.T) {
	first := Open(t)
	second := Open(t)
	if _, err := second.Exec("INSERT INTO revisions(id, desired_revision, applied_revision) VALUES(1, 0, 0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec("INSERT INTO revisions(id, desired_revision, applied_revision) VALUES(1, 41, 0)"); err != nil {
		t.Fatal(err)
	}
	var firstRevision, secondRevision int
	if err := first.QueryRow("SELECT desired_revision FROM revisions WHERE id=1").Scan(&firstRevision); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRow("SELECT desired_revision FROM revisions WHERE id=1").Scan(&secondRevision); err != nil {
		t.Fatal(err)
	}
	if firstRevision != 41 || secondRevision != 0 {
		t.Fatalf("clone mutation leaked: first=%d second=%d", firstRevision, secondRevision)
	}
}

func TestCloneIfMissingUsesProductionOpenForExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE migration_probe(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := CloneIfMissing(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if _, err := opened.Exec("INSERT INTO migration_probe(value) VALUES('ok')"); err != nil {
		t.Fatal(err)
	}
}
