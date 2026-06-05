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
	if !report.Encrypted || report.Legacy || report.FormatVersion != 1 ||
		report.EncryptionVersion != 2 || report.VeilVersion != "0.6.0" ||
		report.StateSchemaVersion != managementstate.CurrentSchemaVersion ||
		!report.CreatedAt.Equal(createdAt) {
		t.Fatalf("verification report = %+v", report)
	}
	if len(report.Files) != 2 || report.Files[0].SHA256 == "" || report.Files[1].SHA256 == "" {
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
	return statePath, keyPath
}
