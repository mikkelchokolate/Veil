package backup

import (
	"path/filepath"
	"testing"
)

func TestBackupPathExistsStatError(t *testing.T) {
	dir := t.TempDir()
	link := makeCircularSymlink(t, dir, "exists")
	exists, err := backupPathExists(link)
	if err == nil {
		t.Fatal("expected stat error")
	}
	if exists {
		t.Fatal("expected exists=false")
	}
}

func TestBackupPathExistsMissing(t *testing.T) {
	exists, err := backupPathExists(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected missing path to not exist")
	}
}
