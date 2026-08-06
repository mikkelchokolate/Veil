package testdb

import (
	"testing"
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
