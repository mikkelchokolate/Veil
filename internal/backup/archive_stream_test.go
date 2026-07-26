package backup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

const largeBackupFixtureBytes = 36 * 1024 * 1024

func TestStreamingArchiveLargerThan32MiBCreateVerifyAndRestore(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	databasePath := filepath.Join(filepath.Dir(statePath), "veil.db")
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE large_backup_fixture(payload BLOB NOT NULL)`,
		`INSERT INTO large_backup_fixture(payload) VALUES(randomblob(` + "37748736" + `))`,
		`INSERT OR REPLACE INTO revisions(id, desired_revision, applied_revision) VALUES(1, 3, 2)`,
		`INSERT INTO revision_snapshots(revision, payload, created_at) VALUES(3, '{"settings":{"domain":"large.example"}}', 1)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 32*1024*1024 {
		t.Fatalf("test database size=%d, want >32 MiB", info.Size())
	}

	archivePath := filepath.Join(t.TempDir(), "large-backup.enc")
	passphrase := strings.Repeat("streaming-pass-", 2)
	if err := CreateBackupFileWithOptions(archivePath, statePath, keyPath, passphrase, ArchiveOptions{DatabasePath: databasePath}); err != nil {
		t.Fatalf("create large streaming backup: %v", err)
	}
	report, err := VerifyBackupFile(archivePath, passphrase, 0)
	if err != nil {
		t.Fatalf("verify large streaming backup: %v", err)
	}
	if report.EncryptionVersion != 3 {
		t.Fatalf("encryption version=%d, want chunked v3", report.EncryptionVersion)
	}
	if report.DesiredRevision != 3 {
		t.Fatalf("desired revision=%d, want 3", report.DesiredRevision)
	}
	var archivedDatabaseSize int64
	for _, file := range report.Files {
		if file.Name == "veil.db" {
			archivedDatabaseSize = file.Size
		}
	}
	if archivedDatabaseSize <= 32*1024*1024 {
		t.Fatalf("archived veil.db size=%d, want >32 MiB", archivedDatabaseSize)
	}

	restoreDir := t.TempDir()
	targetState := filepath.Join(restoreDir, "state.json")
	targetKey := filepath.Join(restoreDir, "state.key")
	targetDatabase := filepath.Join(restoreDir, "veil.db")
	result, err := RestoreBackupFileWithOptions(archivePath, targetState, targetKey, passphrase, RestoreOptions{DatabasePath: targetDatabase})
	if err != nil {
		t.Fatalf("restore large streaming backup: %v", err)
	}
	if !result.Verified || result.CheckOnly {
		t.Fatalf("restore result=%+v", result)
	}
	db, err = storage.OpenExisting(targetDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var restoredBytes int64
	if err := db.QueryRow(`SELECT length(payload) FROM large_backup_fixture`).Scan(&restoredBytes); err != nil {
		t.Fatal(err)
	}
	if restoredBytes != largeBackupFixtureBytes {
		t.Fatalf("restored payload bytes=%d, want %d", restoredBytes, largeBackupFixtureBytes)
	}
}

func TestStreamingArchiveRejectsExplicitExpandedSizePolicy(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	databasePath := filepath.Join(filepath.Dir(statePath), "veil.db")
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE size_policy_fixture(payload BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO size_policy_fixture(payload) VALUES(zeroblob(2097152))`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(t.TempDir(), "too-large.enc")
	err = CreateBackupFileWithOptions(archivePath, statePath, keyPath, strings.Repeat("policy-pass-", 2), ArchiveOptions{
		DatabasePath: databasePath,
		MaxBytes:     1024 * 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "configured backup size policy") {
		t.Fatalf("explicit size policy error=%v", err)
	}
	if _, statErr := os.Stat(archivePath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected archive was published: %v", statErr)
	}
}

func TestStreamingArchiveChunkAuthenticationRejectsTamperTruncationAndTrailingData(t *testing.T) {
	dir := t.TempDir()
	statePath, keyPath := writeValidBackupSource(t)
	archivePath := filepath.Join(dir, "authenticated.enc")
	const passphrase = "streaming-authentication-passphrase"
	if err := CreateBackupFileWithOptions(archivePath, statePath, keyPath, passphrase, ArchiveOptions{}); err != nil {
		t.Fatalf("CreateBackupFileWithOptions: %v", err)
	}
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < len(magicHeader)+1+16+12+32 {
		t.Fatalf("archive unexpectedly short: %d", len(body))
	}

	cases := map[string][]byte{
		"tampered frame":     append([]byte(nil), body...),
		"truncated terminal": append([]byte(nil), body[:len(body)-1]...),
		"trailing data":      append(append([]byte(nil), body...), 0x42),
	}
	cases["tampered frame"][len(magicHeader)+1+16+12+8] ^= 0x80
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".enc")
			if err := os.WriteFile(path, candidate, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyBackupFile(path, passphrase, DefaultMaxBackupBytes); err == nil {
				t.Fatal("expected authenticated streaming verification to reject corrupt archive")
			}
		})
	}
}

func TestConfiguredMaxBackupBytes(t *testing.T) {
	t.Setenv("VEIL_BACKUP_MAX_BYTES", "50331648")
	got, err := ConfiguredMaxBackupBytes()
	if err != nil {
		t.Fatal(err)
	}
	if got != 48*1024*1024 {
		t.Fatalf("configured max=%d want=%d", got, 48*1024*1024)
	}
	t.Setenv("VEIL_BACKUP_MAX_BYTES", "0")
	if _, err := ConfiguredMaxBackupBytes(); err == nil {
		t.Fatal("expected non-positive policy to be rejected")
	}
}

func TestStreamingArchiveRejectsSymlinkedStateAndArchive(t *testing.T) {
	dir := t.TempDir()
	statePath, keyPath := writeValidBackupSource(t)
	databasePath := filepath.Join(filepath.Dir(statePath), "veil.db")
	stateLink := filepath.Join(dir, "linked-state.json")
	if err := os.Symlink(statePath, stateLink); err != nil {
		t.Fatal(err)
	}
	if err := CreateBackupFileWithOptions(filepath.Join(dir, "linked-source.enc"), stateLink, keyPath, "correct horse battery staple", ArchiveOptions{DatabasePath: databasePath}); err == nil {
		t.Fatal("expected symlinked state source to be rejected")
	}
	databaseLink := filepath.Join(dir, "linked-veil.db")
	if err := os.Symlink(databasePath, databaseLink); err != nil {
		t.Fatal(err)
	}
	if err := CreateBackupFileWithOptions(filepath.Join(dir, "linked-database.enc"), statePath, keyPath, "correct horse battery staple", ArchiveOptions{DatabasePath: databaseLink}); err == nil {
		t.Fatal("expected symlinked database source to be rejected")
	}

	archivePath := filepath.Join(dir, "valid.enc")
	if err := CreateBackupFileWithOptions(archivePath, statePath, keyPath, "correct horse battery staple", ArchiveOptions{DatabasePath: databasePath}); err != nil {
		t.Fatal(err)
	}
	archiveLink := filepath.Join(dir, "linked-archive.enc")
	if err := os.Symlink(archivePath, archiveLink); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackupFile(archiveLink, "correct horse battery staple", 0); err == nil {
		t.Fatal("expected symlinked archive to be rejected")
	}
}

func TestStreamingArchiveSnapshotBarrierPreventsMixedStateAndRevision(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	databasePath := filepath.Join(filepath.Dir(statePath), "veil.db")
	preState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var stateDocument map[string]any
	if err := json.Unmarshal(preState, &stateDocument); err != nil {
		t.Fatal(err)
	}
	settings, ok := stateDocument["settings"].(map[string]any)
	if !ok {
		t.Fatal("fixture state has no settings object")
	}
	settings["panelListen"] = "127.0.0.1:3096"
	postState, err := json.Marshal(stateDocument)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(preState, postState) {
		t.Fatal("fixture state mutation did not change the state document")
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO revisions(id, desired_revision, applied_revision) VALUES(1, 1, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO revision_snapshots(revision, payload, created_at) VALUES(1, ?, 1)`, preState); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	stateCaptured := make(chan struct{})
	releaseCapture := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCapture) }) }
	defer release()
	archivePath := filepath.Join(t.TempDir(), "barrier.enc")
	archiveDone := make(chan error, 1)
	go func() {
		archiveDone <- CreateBackupFileWithOptions(archivePath, statePath, keyPath, "correct horse battery staple", ArchiveOptions{
			DatabasePath: databasePath,
			afterStateCapture: func() {
				close(stateCaptured)
				<-releaseCapture
			},
		})
	}()
	<-stateCaptured

	mutationStarted := make(chan struct{})
	mutationEntered := make(chan struct{})
	mutationDone := make(chan error, 1)
	go func() {
		close(mutationStarted)
		mutationDone <- managementstate.WithSnapshotBarrier(statePath, func() error {
			close(mutationEntered)
			if err := os.WriteFile(statePath, postState, 0o600); err != nil {
				return err
			}
			mutationDB, err := storage.OpenExisting(databasePath)
			if err != nil {
				return err
			}
			defer mutationDB.Close()
			if _, err := mutationDB.Exec(`INSERT OR REPLACE INTO revisions(id, desired_revision, applied_revision) VALUES(1, 2, 0)`); err != nil {
				return err
			}
			_, err = mutationDB.Exec(`INSERT OR REPLACE INTO revision_snapshots(revision, payload, created_at) VALUES(2, ?, 2)`, postState)
			return err
		})
	}()
	<-mutationStarted
	select {
	case <-mutationEntered:
		release()
		t.Fatal("configuration mutation entered between state and SQLite capture")
	case <-time.After(150 * time.Millisecond):
	}
	release()
	if err := <-archiveDone; err != nil {
		t.Fatal(err)
	}
	if err := <-mutationDone; err != nil {
		t.Fatal(err)
	}

	report, err := VerifyBackupFile(archivePath, "correct horse battery staple", 0)
	if err != nil {
		t.Fatal(err)
	}
	if report.DesiredRevision != 1 {
		t.Fatalf("archived desired revision=%d want=1", report.DesiredRevision)
	}
	inspected, err := inspectBackupFile(archivePath, "correct horse battery staple", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer inspected.cleanup()
	if !bytes.Equal(inspected.state, preState) {
		t.Fatal("archive mixed post-mutation state with pre-mutation SQLite revision")
	}
}

type shortWriteBuffer struct {
	body bytes.Buffer
	max  int
}

func (w *shortWriteBuffer) Write(body []byte) (int, error) {
	if len(body) > w.max {
		body = body[:w.max]
	}
	return w.body.Write(body)
}

func TestChunkedEncryptionHandlesLegalShortWrites(t *testing.T) {
	plaintext := []byte(strings.Repeat("short-writer-frame-", 4096))
	destination := &shortWriteBuffer{max: 7}
	writer, err := newChunkEncryptWriter(destination, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var decrypted bytes.Buffer
	if err := decryptChunkedBackup(bytes.NewReader(destination.body.Bytes()), &decrypted, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted.Bytes(), plaintext) {
		t.Fatal("chunked round trip changed plaintext through a short writer")
	}
}
