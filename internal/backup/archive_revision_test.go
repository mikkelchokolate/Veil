package backup

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestArchiveManifestCapturesDesiredRevisionAndRequiresImmutableSnapshot(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(filepath.Dir(statePath), "veil.db")
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO revisions(id, desired_revision, applied_revision) VALUES(1, 7, 6)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO revision_snapshots(revision, payload, created_at, state_sha256) VALUES(7, '{"settings":{"domain":"pre.example"}}', 1, ?)`, backupChecksum(stateBody)); err != nil {
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

func TestArchiveVerificationRejectsStateFromDifferentDesiredRevision(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(filepath.Dir(statePath), "veil.db")
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO revisions(id, desired_revision, applied_revision) VALUES(1, 7, 6)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO revision_snapshots(revision, payload, created_at, state_sha256) VALUES(7, '{"settings":{"mode":"server"}}', 1, ?)`, backupChecksum(stateBody)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{DatabasePath: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	mismatched := replaceArchivedStateMode(t, data, "server", "dev")
	if _, err := VerifyBackup(mismatched, ""); err == nil || !strings.Contains(err.Error(), "state digest") {
		t.Fatalf("verify mismatched state/revision error = %v", err)
	}
}

func TestArchiveCreationAndVerificationRejectUnboundV2(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	keyBody, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyDBPath := filepath.Join(t.TempDir(), "legacy-v2.db")
	db, err := sql.Open("sqlite", legacyDBPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE revisions (id INTEGER PRIMARY KEY CHECK (id = 1), desired_revision INTEGER NOT NULL DEFAULT 0, applied_revision INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO revisions(id, desired_revision, applied_revision) VALUES(1, 4, 3)`,
		`CREATE TABLE revision_snapshots (revision INTEGER PRIMARY KEY, payload TEXT NOT NULL, created_at INTEGER NOT NULL DEFAULT (unixepoch()))`,
		`INSERT INTO revision_snapshots(revision, payload, created_at) VALUES(4, '{}', 1)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{DatabasePath: legacyDBPath}); err == nil || !strings.Contains(err.Error(), "state digest binding") {
		t.Fatalf("new capture from unbound legacy DB error=%v", err)
	}

	databaseBody, err := os.ReadFile(legacyDBPath)
	if err != nil {
		t.Fatal(err)
	}
	revision := uint64(4)
	manifest := ArchiveManifest{
		FormatVersion:      CurrentArchiveFormatVersion,
		StateSchemaVersion: managementstate.CurrentSchemaVersion,
		CreatedAt:          time.Unix(1, 0).UTC(),
		DesiredRevision:    &revision,
		Files: []ArchiveFile{
			{Name: "state.json", Size: int64(len(stateBody)), SHA256: backupChecksum(stateBody)},
			{Name: "state.key", Size: int64(len(keyBody)), SHA256: backupChecksum(keyBody)},
			{Name: "veil.db", Size: int64(len(databaseBody)), SHA256: backupChecksum(databaseBody)},
		},
	}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	tarball, err := writeArchiveTarball(archiveContents{state: stateBody, key: keyBody, database: databaseBody, manifest: manifestBody})
	if err != nil {
		t.Fatal(err)
	}
	legacyArchive, err := encryptBackupTarball(tarball, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackup(legacyArchive, ""); err == nil || !strings.Contains(err.Error(), "state digest binding") {
		t.Fatalf("verify unbound archive-v2 error=%v", err)
	}
}

func replaceArchivedStateMode(t *testing.T, data []byte, from, to string) []byte {
	t.Helper()
	tarball, _, _, err := decryptBackup(data, "")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := readArchiveTarball(tarball)
	if err != nil {
		t.Fatal(err)
	}
	before := []byte(`"mode": "` + from + `"`)
	after := []byte(`"mode": "` + to + `"`)
	contents.state = bytes.Replace(contents.state, before, after, 1)
	if bytes.Contains(contents.state, before) || !bytes.Contains(contents.state, after) {
		t.Fatalf("state fixture did not contain replaceable mode %q", from)
	}

	var manifest ArchiveManifest
	if err := json.Unmarshal(contents.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Files {
		if manifest.Files[i].Name == "state.json" {
			manifest.Files[i].Size = int64(len(contents.state))
			manifest.Files[i].SHA256 = backupChecksum(contents.state)
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
