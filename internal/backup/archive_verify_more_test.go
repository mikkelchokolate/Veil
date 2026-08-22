package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestCreateBackupWithOptionsErrors(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")

	if _, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{}); err == nil {
		t.Fatal("expected error for missing state file")
	}

	if err := os.WriteFile(statePath, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{}); err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestInspectBackupValidation(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	validTarball, err := createTarballWithManifest(statePath, keyPath, ArchiveOptions{
		VeilVersion: "0.6.0",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := readArchiveTarball(validTarball)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		mutate  func(*archiveContents)
		wantErr string
	}{
		{
			name: "missing state.json",
			mutate: func(c *archiveContents) {
				c.state = nil
			},
			wantErr: "missing state.json",
		},
		{
			name: "missing state.key",
			mutate: func(c *archiveContents) {
				c.key = nil
			},
			wantErr: "missing state.key",
		},
		{
			name: "future state schema",
			mutate: func(c *archiveContents) {
				c.state = []byte(`{"schemaVersion":` + strconv.Itoa(managementstate.CurrentSchemaVersion+1) + `}`)
			},
			wantErr: "newer state schema",
		},
		{
			name: "manifest decode error",
			mutate: func(c *archiveContents) {
				c.manifest = []byte("not json")
			},
			wantErr: "decode backup manifest",
		},
		{
			name: "manifest version mismatch",
			mutate: func(c *archiveContents) {
				c.manifest, _ = json.Marshal(ArchiveManifest{
					FormatVersion:      999,
					StateSchemaVersion: managementstate.CurrentSchemaVersion,
				})
			},
			wantErr: "unsupported archive manifest version",
		},
		{
			name: "manifest schema mismatch",
			mutate: func(c *archiveContents) {
				c.manifest, _ = json.Marshal(ArchiveManifest{
					FormatVersion:      CurrentArchiveFormatVersion,
					StateSchemaVersion: 1,
				})
			},
			wantErr: "state schema mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := contents
			tt.mutate(&mutated)
			data, err := writeArchiveTarball(mutated)
			if err != nil {
				t.Fatal(err)
			}
			_, err = inspectBackup(data, "")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateStateAndKeyErrors(t *testing.T) {
	statePath, _ := writeValidBackupSource(t)
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// Create a state that actually contains encrypted secrets so a mismatched key
	// fails during decryption.
	var secretKey [secrets.KeySize]byte
	copy(secretKey[:], bytes.Repeat([]byte{0x5a}, secrets.KeySize))
	secretCipher, err := secrets.NewCipher(secretKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	secretStatePath := filepath.Join(dir, "state.json")
	secretKeyPath := filepath.Join(dir, "state.key")
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen:   "127.0.0.1:2096",
			PanelAccess:   "local",
			Mode:          "server",
			NaivePassword: "secret-password",
		},
	}
	if err := managementstate.NewStore(secretStatePath, secretCipher).Save(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretKeyPath, secretKey[:], 0o600); err != nil {
		t.Fatal(err)
	}
	secretState, err := os.ReadFile(secretStatePath)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		key     []byte
		state   []byte
		wantErr string
	}{
		{
			name:    "wrong key size",
			key:     bytes.Repeat([]byte{0x42}, 16),
			state:   state,
			wantErr: "state.key length is",
		},
		{
			name:    "state decode error",
			key:     bytes.Repeat([]byte{0x42}, secrets.KeySize),
			state:   []byte("not valid json"),
			wantErr: "invalid character",
		},
		{
			name:    "state and key do not match",
			key:     bytes.Repeat([]byte{0x42}, secrets.KeySize),
			state:   secretState,
			wantErr: "state and key do not match",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStateAndKey(tt.state, tt.key)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestRestoreBackupWithOptionsStagingErrors(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	data, err := CreateBackupWithOptions(statePath, keyPath, "", ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("state mkdir fails", func(t *testing.T) {
		targetDir := t.TempDir()
		targetState := filepath.Join(targetDir, "state.json")
		targetKey := filepath.Join(targetDir, "state.key")

		original := restoreMkdirAll
		defer func() { restoreMkdirAll = original }()
		restoreMkdirAll = func(path string, perm os.FileMode) error {
			return errors.New("injected mkdir failure")
		}
		if _, err := RestoreBackupWithOptions(data, targetState, targetKey, "", RestoreOptions{}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("key staging fails after state staged", func(t *testing.T) {
		targetDir := t.TempDir()
		targetState := filepath.Join(targetDir, "state.json")
		targetKey := filepath.Join(targetDir, "state.key")

		originalCreateTemp := restoreCreateTemp
		originalRemove := restoreRemove
		defer func() {
			restoreCreateTemp = originalCreateTemp
			restoreRemove = originalRemove
		}()

		calls := 0
		restoreCreateTemp = func(dir, pattern string) (*os.File, error) {
			calls++
			if calls == 2 {
				return nil, errors.New("injected key temp failure")
			}
			return originalCreateTemp(dir, pattern)
		}
		restoreRemove = func(name string) error {
			// Allow cleanup of the staged state temp to succeed.
			_ = originalRemove(name)
			return nil
		}

		if _, err := RestoreBackupWithOptions(data, targetState, targetKey, "", RestoreOptions{}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("state commit fails", func(t *testing.T) {
		targetDir := t.TempDir()
		targetState := filepath.Join(targetDir, "state.json")
		targetKey := filepath.Join(targetDir, "state.key")

		originalRename := restoreRename
		defer func() { restoreRename = originalRename }()

		calls := 0
		restoreRename = func(oldpath, newpath string) error {
			calls++
			if calls == 1 {
				return errors.New("injected rename failure")
			}
			return originalRename(oldpath, newpath)
		}

		if _, err := RestoreBackupWithOptions(data, targetState, targetKey, "", RestoreOptions{}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("key commit fails triggers rollback", func(t *testing.T) {
		targetDir := t.TempDir()
		targetState := filepath.Join(targetDir, "state.json")
		targetKey := filepath.Join(targetDir, "state.key")

		originalRename := restoreRename
		defer func() { restoreRename = originalRename }()

		calls := 0
		restoreRename = func(oldpath, newpath string) error {
			calls++
			if calls == 2 {
				return errors.New("injected key commit failure")
			}
			return originalRename(oldpath, newpath)
		}

		if _, err := RestoreBackupWithOptions(data, targetState, targetKey, "", RestoreOptions{}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("database commit fails rolls back all three originals", func(t *testing.T) {
		sourceState, sourceKey := writeValidBackupSource(t)
		v2Data, err := CreateBackupWithOptions(sourceState, sourceKey, "", ArchiveOptions{})
		if err != nil {
			t.Fatal(err)
		}
		targetDir := t.TempDir()
		targetState := filepath.Join(targetDir, "state.json")
		targetKey := filepath.Join(targetDir, "state.key")
		targetDB := filepath.Join(targetDir, "veil.db")
		originals := map[string][]byte{targetState: []byte("old-state"), targetKey: []byte("old-key")}
		for path, body := range originals {
			if err := os.WriteFile(path, body, 0o640); err != nil {
				t.Fatal(err)
			}
		}
		oldDB, err := storage.Open(targetDB)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := oldDB.Exec(`INSERT INTO migration_markers(key, version, applied_at, details) VALUES ('old-marker', 1, 0, '{}')`); err != nil {
			t.Fatal(err)
		}
		if err := oldDB.Close(); err != nil {
			t.Fatal(err)
		}
		originalRename := restoreRename
		defer func() { restoreRename = originalRename }()
		restoreRename = func(oldpath, newpath string) error {
			if newpath == targetDB && strings.Contains(filepath.Base(oldpath), ".veil-restore-") {
				return errors.New("injected database commit failure")
			}
			return originalRename(oldpath, newpath)
		}
		if _, err := RestoreBackupWithOptions(v2Data, targetState, targetKey, "", RestoreOptions{DatabasePath: targetDB}); err == nil {
			t.Fatal("expected error")
		}
		for path, want := range originals {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("rollback %s = %q err=%v, want %q", path, got, err, want)
			}
		}
		reopened, err := storage.Open(targetDB)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		var count int
		if err := reopened.QueryRow(`SELECT COUNT(*) FROM migration_markers WHERE key = 'old-marker'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("database rollback marker count=%d err=%v", count, err)
		}
	})
}

func TestStageRestoreFileErrors(t *testing.T) {
	t.Run("mkdir fails", func(t *testing.T) {
		original := restoreMkdirAll
		defer func() { restoreMkdirAll = original }()
		restoreMkdirAll = func(path string, perm os.FileMode) error {
			return errors.New("injected mkdir failure")
		}
		if _, err := stageRestoreFile("/x/state.json", []byte("body"), "/x/state.safety"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("create temp fails", func(t *testing.T) {
		original := restoreCreateTemp
		defer func() { restoreCreateTemp = original }()
		restoreCreateTemp = func(dir, pattern string) (*os.File, error) {
			return nil, errors.New("injected temp failure")
		}
		dir := t.TempDir()
		if _, err := stageRestoreFile(filepath.Join(dir, "state.json"), []byte("body"), filepath.Join(dir, "safety")); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("chmod fails cleans up temp", func(t *testing.T) {
		original := restoreChmod
		defer func() { restoreChmod = original }()
		restoreChmod = func(name string, mode os.FileMode) error {
			return errors.New("injected chmod failure")
		}
		dir := t.TempDir()
		if _, err := stageRestoreFile(filepath.Join(dir, "state.json"), []byte("body"), filepath.Join(dir, "safety")); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestStagedRestoreFileRollbackAndCleanup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	safety := filepath.Join(dir, "state.safety")
	original := []byte("original state")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := stageRestoreFile(target, []byte("new state"), safety)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a commit that moved the original to safety and temp to target,
	// then rollback should restore the original and clean up.
	originalRename := restoreRename
	defer func() { restoreRename = originalRename }()
	restoreRename = func(oldpath, newpath string) error {
		return originalRename(oldpath, newpath)
	}
	if err := originalRename(target, safety); err != nil {
		t.Fatal(err)
	}
	if err := originalRename(f.temp, target); err != nil {
		t.Fatal(err)
	}
	f.committed = true

	if err := f.rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("original not restored: %q", got)
	}
}

func TestStagedRestoreFileCleanupStaged(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	f, err := stageRestoreFile(target, []byte("body"), filepath.Join(dir, "safety"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.temp); err != nil {
		t.Fatalf("temp should exist: %v", err)
	}
	if err := f.cleanupStaged(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.temp); !os.IsNotExist(err) {
		t.Fatalf("temp should be removed: %v", err)
	}
}

func TestStagedRestoreFileCommitOriginalRenameError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	safety := filepath.Join(dir, "state.safety")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := stageRestoreFile(target, []byte("new"), safety)
	if err != nil {
		t.Fatal(err)
	}

	origRename := restoreRename
	defer func() { restoreRename = origRename }()
	restoreRename = func(string, string) error {
		return errors.New("injected original rename error")
	}

	if err := f.commit(); err == nil {
		t.Fatal("expected error")
	}
}

func TestStagedRestoreFileCommitRollback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	safety := filepath.Join(dir, "state.safety")
	original := []byte("original state")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := stageRestoreFile(target, []byte("new state"), safety)
	if err != nil {
		t.Fatal(err)
	}

	origRename := restoreRename
	defer func() { restoreRename = origRename }()
	calls := 0
	restoreRename = func(oldpath, newpath string) error {
		calls++
		if calls == 2 {
			return errors.New("injected rename error")
		}
		return origRename(oldpath, newpath)
	}

	if err := f.commit(); err == nil {
		t.Fatal("expected error")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("original not restored after failed commit: %q", got)
	}
}

func TestInspectBackupValidateStateAndKeyError(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")

	var secretKey [secrets.KeySize]byte
	copy(secretKey[:], bytes.Repeat([]byte{0x5a}, secrets.KeySize))
	cipher, err := secrets.NewCipher(secretKey)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen:   "127.0.0.1:2096",
			PanelAccess:   "local",
			Mode:          "server",
			NaivePassword: "secret-password",
		},
	}
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, secretKey[:], 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	// A valid-length key that does not match the encrypted state.
	badKey := bytes.Repeat([]byte{0x42}, 32)
	contents := archiveContents{state: state, key: badKey}
	data, err := writeArchiveTarball(contents)
	if err != nil {
		t.Fatal(err)
	}
	_, err = inspectBackup(data, "")
	if err == nil || !strings.Contains(err.Error(), "validate backup state") {
		t.Fatalf("expected validate error, got %v", err)
	}
}

func TestInspectBackupReadArchiveError(t *testing.T) {
	_, err := inspectBackup([]byte("not a valid archive"), "")
	if err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Fatalf("expected gzip reader error, got %v", err)
	}
}

func TestReadArchiveTarballGzipError(t *testing.T) {
	_, err := readArchiveTarball([]byte("not gzip"))
	if err == nil || !strings.Contains(err.Error(), "initialize gzip reader") {
		t.Fatalf("expected gzip init error, got %v", err)
	}
}

func TestReadArchiveTarballCorruptTar(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte("not a tar header")); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := readArchiveTarball(buf.Bytes())
	if err == nil || !strings.Contains(err.Error(), "read tar archive") {
		t.Fatalf("expected tar read error, got %v", err)
	}
}

func TestCreateTarballWithManifestMarshalError(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	orig := archiveManifestMarshal
	defer func() { archiveManifestMarshal = orig }()
	archiveManifestMarshal = func(any) ([]byte, error) {
		return nil, errors.New("injected marshal error")
	}
	if _, err := createTarballWithManifest(statePath, keyPath, ArchiveOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteArchiveTarballErrors(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	contents := archiveContents{
		state:    state,
		key:      key,
		manifest: []byte(`{"formatVersion":1}`),
	}

	tests := []struct {
		name    string
		inject  func() (cleanup func())
		wantErr string
	}{
		{
			name: "write header error",
			inject: func() (cleanup func()) {
				orig := archiveWriteHeader
				archiveWriteHeader = func(*tar.Writer, *tar.Header) error {
					return errors.New("injected write header error")
				}
				return func() { archiveWriteHeader = orig }
			},
			wantErr: "injected write header error",
		},
		{
			name: "write error",
			inject: func() (cleanup func()) {
				orig := archiveWrite
				archiveWrite = func(*tar.Writer, []byte) (int, error) {
					return 0, errors.New("injected write error")
				}
				return func() { archiveWrite = orig }
			},
			wantErr: "injected write error",
		},
		{
			name: "tar close error",
			inject: func() (cleanup func()) {
				orig := archiveClose
				archiveClose = func(*tar.Writer) error {
					return errors.New("injected tar close error")
				}
				return func() { archiveClose = orig }
			},
			wantErr: "injected tar close error",
		},
		{
			name: "gzip close error",
			inject: func() (cleanup func()) {
				orig := archiveGzipClose
				archiveGzipClose = func(*gzip.Writer) error {
					return errors.New("injected gzip close error")
				}
				return func() { archiveGzipClose = orig }
			},
			wantErr: "injected gzip close error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := tt.inject()
			defer cleanup()
			if _, err := writeArchiveTarball(contents); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDecryptBackupCipherErrors(t *testing.T) {
	data := append(bytes.Clone(magicHeader), byte(2))
	data = append(data, make([]byte, 28)...)

	t.Run("aes new cipher error", func(t *testing.T) {
		orig := decryptAESNewCipher
		defer func() { decryptAESNewCipher = orig }()
		decryptAESNewCipher = func([]byte) (cipher.Block, error) {
			return nil, errors.New("injected aes error")
		}
		if _, _, _, err := decryptBackup(data, "passphrase"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("gcm error", func(t *testing.T) {
		orig := decryptNewGCM
		defer func() { decryptNewGCM = orig }()
		decryptNewGCM = func(cipher.Block) (cipher.AEAD, error) {
			return nil, errors.New("injected gcm error")
		}
		if _, _, _, err := decryptBackup(data, "passphrase"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestValidateStateAndKeyNewCipherError(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	orig := validateSecretsNewCipher
	defer func() { validateSecretsNewCipher = orig }()
	validateSecretsNewCipher = func([secrets.KeySize]byte) (*secrets.Cipher, error) {
		return nil, errors.New("injected cipher error")
	}

	if err := validateStateAndKey(state, key); err == nil {
		t.Fatal("expected error")
	}
}

func TestStageRestoreFileWriteSyncCloseErrors(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	safety := filepath.Join(dir, "safety")

	tests := []struct {
		name    string
		inject  func() (cleanup func())
		wantErr string
	}{
		{
			name: "write error",
			inject: func() (cleanup func()) {
				orig := restoreFileWrite
				restoreFileWrite = func(*os.File, []byte) (int, error) {
					return 0, errors.New("injected write error")
				}
				return func() { restoreFileWrite = orig }
			},
			wantErr: "injected write error",
		},
		{
			name: "sync error",
			inject: func() (cleanup func()) {
				orig := restoreFileSync
				restoreFileSync = func(*os.File) error {
					return errors.New("injected sync error")
				}
				return func() { restoreFileSync = orig }
			},
			wantErr: "injected sync error",
		},
		{
			name: "close error",
			inject: func() (cleanup func()) {
				orig := restoreFileClose
				restoreFileClose = func(*os.File) error {
					return errors.New("injected close error")
				}
				return func() { restoreFileClose = orig }
			},
			wantErr: "injected close error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := tt.inject()
			defer cleanup()
			_, err := stageRestoreFile(target, []byte("body"), safety)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
