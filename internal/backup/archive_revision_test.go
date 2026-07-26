package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestArchiveManifestCapturesDesiredRevisionAndRequiresImmutableSnapshot(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	databasePath := filepath.Join(filepath.Dir(statePath), "veil.db")
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO revisions(id, desired_revision, applied_revision) VALUES(1, 7, 6)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO revision_snapshots(revision, payload, created_at) VALUES(7, '{"settings":{"domain":"pre.example"}}', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{DatabasePath: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	report, err := VerifyBackup(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.DesiredRevision != 7 {
		t.Fatalf("captured desired revision = %d, want 7", report.DesiredRevision)
	}

	withoutSnapshot := removeArchivedRevisionSnapshot(t, data, 7)
	if _, err := VerifyBackup(withoutSnapshot, ""); err == nil || !strings.Contains(err.Error(), "immutable snapshot for desired revision 7") {
		t.Fatalf("verify without captured revision snapshot error = %v", err)
	}
}

func removeArchivedRevisionSnapshot(t *testing.T, data []byte, revision uint64) []byte {
	t.Helper()
	tarball, _, _, err := decryptBackup(data, "")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := readArchiveTarball(tarball)
	if err != nil {
		t.Fatal(err)
	}

	databasePath := filepath.Join(t.TempDir(), "veil.db")
	if err := os.WriteFile(databasePath, contents.database, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM revision_snapshots WHERE revision = ?`, revision); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	contents.database, err = os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}

	var manifest ArchiveManifest
	if err := json.Unmarshal(contents.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Files {
		if manifest.Files[i].Name == "veil.db" {
			manifest.Files[i].Size = int64(len(contents.database))
			manifest.Files[i].SHA256 = backupChecksum(contents.database)
		}
	}
	contents.manifest, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	tarball, err = writeArchiveTarball(contents)
	if err != nil {
		t.Fatal(err)
	}
	result, err := encryptBackupTarball(tarball, "")
	if err != nil {
		t.Fatal(err)
	}
	return result
}
