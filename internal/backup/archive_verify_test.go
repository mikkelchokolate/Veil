package backup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestVerifyBackupReportsManifestAndCompatibility(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	createdAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	data, err := CreateBackupWithOptions(statePath, keyPath, "backup-passphrase", ArchiveOptions{
		VeilVersion: "0.6.0",
		CreatedAt:   createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := VerifyBackup(data, "backup-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Encrypted || report.Legacy || report.FormatVersion != CurrentArchiveFormatVersion ||
		report.EncryptionVersion != 2 || report.VeilVersion != "0.6.0" ||
		report.StateSchemaVersion != managementstate.CurrentSchemaVersion ||
		!report.CreatedAt.Equal(createdAt) {
		t.Fatalf("verification report = %+v", report)
	}
	if len(report.Files) != 3 || report.Files[0].SHA256 == "" || report.Files[1].SHA256 == "" || report.Files[2].SHA256 == "" {
		t.Fatalf("verified files = %+v", report.Files)
	}
}

func TestVerifyBackupRejectsManifestChecksumMismatch(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	data, err := createTarballWithManifest(statePath, keyPath, ArchiveOptions{
		VeilVersion: "0.6.0",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := readArchiveTarball(data)
	if err != nil {
		t.Fatal(err)
	}
	var manifest ArchiveManifest
	if err := json.Unmarshal(contents.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files[0].SHA256 = strings.Repeat("0", 64)
	contents.manifest, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	data, err = writeArchiveTarball(contents)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := VerifyBackup(data, ""); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum verification error, got %v", err)
	}
}

func TestVerifyBackupRejectsFutureStateSchema(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	if err := os.WriteFile(statePath, []byte(`{"schemaVersion":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x42}, secrets.KeySize), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(dir, "veil.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{
		VeilVersion: "future",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := VerifyBackup(data, ""); err == nil || !strings.Contains(err.Error(), "newer state schema") {
		t.Fatalf("expected future schema rejection, got %v", err)
	}
}

func TestRestoreBackupCheckOnlyDoesNotWrite(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	data, err := CreateBackupWithOptions(statePath, keyPath, "backup-passphrase", ArchiveOptions{
		VeilVersion: "0.6.0",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	targetState := filepath.Join(targetDir, "state.json")
	targetKey := filepath.Join(targetDir, "state.key")

	result, err := RestoreBackupWithOptions(data, targetState, targetKey, "backup-passphrase", RestoreOptions{
		CheckOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || !result.CheckOnly {
		t.Fatalf("restore result = %+v", result)
	}
	if _, err := os.Stat(targetState); !os.IsNotExist(err) {
		t.Fatalf("check-only wrote state: %v", err)
	}
	if _, err := os.Stat(targetKey); !os.IsNotExist(err) {
		t.Fatalf("check-only wrote key: %v", err)
	}
}

func TestRestoreBackupPreservesPreviousStateAndKey(t *testing.T) {
	sourceState, sourceKey := writeValidBackupSource(t)
	data, err := CreateBackupWithOptions(sourceState, sourceKey, "", ArchiveOptions{
		VeilVersion: "0.6.0",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	targetState := filepath.Join(targetDir, "state.json")
	targetKey := filepath.Join(targetDir, "state.key")
	oldState := []byte("old state")
	oldKey := bytes.Repeat([]byte{0x11}, secrets.KeySize)
	if err := os.WriteFile(targetState, oldState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetKey, oldKey, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := RestoreBackupWithOptions(data, targetState, targetKey, "", RestoreOptions{
		Now: func() time.Time {
			return time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SafetyStatePath == "" || result.SafetyKeyPath == "" {
		t.Fatalf("restore result = %+v", result)
	}
	gotOldState, err := os.ReadFile(result.SafetyStatePath)
	if err != nil {
		t.Fatal(err)
	}
	gotOldKey, err := os.ReadFile(result.SafetyKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOldState, oldState) || !bytes.Equal(gotOldKey, oldKey) {
		t.Fatalf("safety backup state=%q key=%x", gotOldState, gotOldKey)
	}
	if _, err := VerifyBackup(data, ""); err != nil {
		t.Fatalf("source archive no longer verifies: %v", err)
	}
}

func TestArchiveV2RestoreRequiresIdleSQLiteBoundary(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	data, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	targetState, targetKey := filepath.Join(targetDir, "state.json"), filepath.Join(targetDir, "state.key")
	if err := os.WriteFile(targetState, []byte(`{"schemaVersion":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetKey, bytes.Repeat([]byte{1}, secrets.KeySize), 0o600); err != nil {
		t.Fatal(err)
	}
	targetDB := filepath.Join(targetDir, "veil.db")
	db, err := storage.Open(targetDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO migration_markers(key, version, applied_at, details) VALUES ('busy', 1, 0, '{}')`); err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := RestoreBackupWithOptions(data, targetState, targetKey, "", RestoreOptions{DatabasePath: targetDB}); err == nil {
		t.Fatal("restore succeeded while target database had an active writer")
	}
}

func TestArchiveV2RestoresNormalizedSQLiteDomain(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	dbPath := filepath.Join(filepath.Dir(statePath), "veil.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT OR REPLACE INTO revisions(id,desired_revision,applied_revision) VALUES(1,7,6)`,
		`INSERT INTO clients(id,name,enabled,quota_reset_policy,created_at,updated_at) VALUES('client-1','alice',1,'never',1,1)`,
		`INSERT INTO client_bindings(id,client_id,inbound_id,enabled,created_at,updated_at) VALUES('binding-1','client-1','hy2',1,1,1)`,
		`INSERT INTO client_credentials(id,binding_id,kind,encrypted_value,created_at) VALUES('credential-1','binding-1','password',X'0102',1)`,
		`INSERT INTO subscription_tokens(id,client_id,token_hash,token_prefix,created_at) VALUES('token-1','client-1',X'0304','tok',1)`,
		`INSERT INTO traffic_counters(client_id,binding_id,upload_bytes,download_bytes,updated_at) VALUES('client-1','binding-1',11,22,1)`,
		`INSERT INTO revision_snapshots(revision,payload,created_at) VALUES(7,'{"clients":["client-1"]}',1)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	report, err := VerifyBackup(data, "")
	if err != nil || report.FormatVersion != 2 || len(report.Files) != 3 {
		t.Fatalf("v2 report: %+v err=%v", report, err)
	}
	db, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"client_credentials", "subscription_tokens", "traffic_counters", "revision_snapshots", "client_bindings", "clients"} {
		if _, err := db.Exec(`DELETE FROM ` + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE revisions SET desired_revision=0, applied_revision=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := RestoreBackupWithOptions(data, statePath, keyPath, "", RestoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.SafetyDatabasePath == "" {
		t.Fatal("missing veil.db safety copy")
	}
	db, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"clients", "client_bindings", "client_credentials", "subscription_tokens", "traffic_counters", "revision_snapshots"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var desired, applied int
	if err := db.QueryRow(`SELECT desired_revision, applied_revision FROM revisions WHERE id=1`).Scan(&desired, &applied); err != nil || desired != 7 || applied != 6 {
		t.Fatalf("revisions=%d/%d err=%v", desired, applied, err)
	}
}

func writeValidBackupSource(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	var key [secrets.KeySize]byte
	copy(key[:], bytes.Repeat([]byte{0x5a}, secrets.KeySize))
	if err := os.WriteFile(keyPath, key[:], 0o600); err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen: "127.0.0.1:2096",
			PanelAccess: "local",
			Mode:        "server",
		},
	}
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(dir, "veil.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return statePath, keyPath
}
