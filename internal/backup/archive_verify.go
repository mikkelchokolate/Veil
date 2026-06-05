package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

const (
	CurrentArchiveFormatVersion = 1
	maxBackupArchiveFileBytes   = 32 * 1024 * 1024
)

type ArchiveOptions struct {
	VeilVersion string
	CreatedAt   time.Time
}

type ArchiveFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ArchiveManifest struct {
	FormatVersion      int           `json:"formatVersion"`
	CreatedAt          time.Time     `json:"createdAt"`
	VeilVersion        string        `json:"veilVersion"`
	StateSchemaVersion int           `json:"stateSchemaVersion"`
	Files              []ArchiveFile `json:"files"`
}

type VerificationReport struct {
	FormatVersion      int           `json:"formatVersion"`
	EncryptionVersion  int           `json:"encryptionVersion"`
	Encrypted          bool          `json:"encrypted"`
	Legacy             bool          `json:"legacy"`
	CreatedAt          time.Time     `json:"createdAt,omitempty"`
	VeilVersion        string        `json:"veilVersion,omitempty"`
	StateSchemaVersion int           `json:"stateSchemaVersion"`
	Files              []ArchiveFile `json:"files"`
}

type RestoreOptions struct {
	CheckOnly bool
	Now       func() time.Time
}

type RestoreResult struct {
	Verified        bool               `json:"verified"`
	CheckOnly       bool               `json:"checkOnly"`
	Verification    VerificationReport `json:"verification"`
	SafetyStatePath string             `json:"safetyStatePath,omitempty"`
	SafetyKeyPath   string             `json:"safetyKeyPath,omitempty"`
}

type archiveContents struct {
	state    []byte
	key      []byte
	manifest []byte
}

type verifiedBackup struct {
	report VerificationReport
	state  []byte
	key    []byte
}

func CreateBackupWithOptions(statePath, keyPath, passphrase string, options ArchiveOptions) ([]byte, error) {
	tarball, err := createTarballWithManifest(statePath, keyPath, options)
	if err != nil {
		return nil, err
	}
	return encryptBackupTarball(tarball, passphrase)
}

func createTarballWithManifest(statePath, keyPath string, options ArchiveOptions) ([]byte, error) {
	state, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("archive state: %w", err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("archive key: %w", err)
	}
	createdAt := options.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	veilVersion := strings.TrimSpace(options.VeilVersion)
	if veilVersion == "" {
		veilVersion = "unknown"
	}
	manifest := ArchiveManifest{
		FormatVersion:      CurrentArchiveFormatVersion,
		CreatedAt:          createdAt,
		VeilVersion:        veilVersion,
		StateSchemaVersion: rawStateSchemaVersion(state),
		Files: []ArchiveFile{
			{Name: "state.json", Size: int64(len(state)), SHA256: backupChecksum(state)},
			{Name: "state.key", Size: int64(len(key)), SHA256: backupChecksum(key)},
		},
	}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal backup manifest: %w", err)
	}
	return writeArchiveTarball(archiveContents{state: state, key: key, manifest: manifestBody})
}

func VerifyBackup(data []byte, passphrase string) (VerificationReport, error) {
	verified, err := inspectBackup(data, passphrase)
	if err != nil {
		return VerificationReport{}, err
	}
	return verified.report, nil
}

func RestoreBackupWithOptions(data []byte, statePath, keyPath, passphrase string, options RestoreOptions) (RestoreResult, error) {
	verified, err := inspectBackup(data, passphrase)
	if err != nil {
		return RestoreResult{}, err
	}
	result := RestoreResult{
		Verified:     true,
		CheckOnly:    options.CheckOnly,
		Verification: verified.report,
	}
	if options.CheckOnly {
		return result, nil
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	suffix := now().UTC().Format("20060102T150405.000000000Z")
	stateSafety := statePath + ".pre-restore-" + suffix
	keySafety := keyPath + ".pre-restore-" + suffix
	stateBackup, err := stageRestoreFile(statePath, verified.state, stateSafety)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("stage state restore: %w", err)
	}
	keyBackup, err := stageRestoreFile(keyPath, verified.key, keySafety)
	if err != nil {
		_ = stateBackup.rollback()
		return RestoreResult{}, fmt.Errorf("stage key restore: %w", err)
	}
	if err := stateBackup.commit(); err != nil {
		_ = keyBackup.cleanupStaged()
		return RestoreResult{}, fmt.Errorf("replace state: %w", err)
	}
	if err := keyBackup.commit(); err != nil {
		_ = stateBackup.rollback()
		_ = keyBackup.rollback()
		return RestoreResult{}, fmt.Errorf("replace key: %w", err)
	}
	if stateBackup.hadOriginal {
		result.SafetyStatePath = stateSafety
	}
	if keyBackup.hadOriginal {
		result.SafetyKeyPath = keySafety
	}
	return result, nil
}

func inspectBackup(data []byte, passphrase string) (verifiedBackup, error) {
	tarball, encrypted, encryptionVersion, err := decryptBackup(data, passphrase)
	if err != nil {
		return verifiedBackup{}, err
	}
	contents, err := readArchiveTarball(tarball)
	if err != nil {
		return verifiedBackup{}, err
	}
	if len(contents.state) == 0 {
		return verifiedBackup{}, errors.New("invalid backup: missing state.json")
	}
	if len(contents.key) == 0 {
		return verifiedBackup{}, errors.New("invalid backup: missing state.key")
	}
	sourceSchema := rawStateSchemaVersion(contents.state)
	if sourceSchema > managementstate.CurrentSchemaVersion {
		return verifiedBackup{}, fmt.Errorf(
			"backup uses newer state schema %d; this Veil supports up to %d",
			sourceSchema,
			managementstate.CurrentSchemaVersion,
		)
	}
	if err := validateStateAndKey(contents.state, contents.key); err != nil {
		return verifiedBackup{}, fmt.Errorf("validate backup state: %w", err)
	}

	report := VerificationReport{
		EncryptionVersion:  encryptionVersion,
		Encrypted:          encrypted,
		Legacy:             len(contents.manifest) == 0,
		StateSchemaVersion: sourceSchema,
		Files: []ArchiveFile{
			{Name: "state.json", Size: int64(len(contents.state)), SHA256: backupChecksum(contents.state)},
			{Name: "state.key", Size: int64(len(contents.key)), SHA256: backupChecksum(contents.key)},
		},
	}
	if len(contents.manifest) != 0 {
		var manifest ArchiveManifest
		if err := json.Unmarshal(contents.manifest, &manifest); err != nil {
			return verifiedBackup{}, fmt.Errorf("decode backup manifest: %w", err)
		}
		if manifest.FormatVersion != CurrentArchiveFormatVersion {
			return verifiedBackup{}, fmt.Errorf("unsupported archive manifest version: %d", manifest.FormatVersion)
		}
		if manifest.StateSchemaVersion != sourceSchema {
			return verifiedBackup{}, fmt.Errorf(
				"backup manifest state schema mismatch: manifest=%d archive=%d",
				manifest.StateSchemaVersion,
				sourceSchema,
			)
		}
		if err := verifyManifestFiles(manifest.Files, report.Files); err != nil {
			return verifiedBackup{}, err
		}
		report.FormatVersion = manifest.FormatVersion
		report.CreatedAt = manifest.CreatedAt.UTC()
		report.VeilVersion = manifest.VeilVersion
		report.Files = manifest.Files
	}
	return verifiedBackup{report: report, state: contents.state, key: contents.key}, nil
}

func decryptBackup(data []byte, passphrase string) ([]byte, bool, int, error) {
	if len(data) < len(magicHeader) || !bytes.Equal(data[:len(magicHeader)], magicHeader) {
		if passphrase != "" {
			return nil, false, 0, errors.New("passphrase provided but backup is not encrypted")
		}
		return data, false, 0, nil
	}
	if passphrase == "" {
		return nil, true, 0, errors.New("passphrase is required to decrypt this backup")
	}
	headerLen := len(magicHeader) + 1 + 16 + 12
	if len(data) < headerLen {
		return nil, true, 0, errors.New("invalid or corrupted encrypted backup file (too short)")
	}
	version := int(data[len(magicHeader)])
	if version != 1 && version != 2 {
		return nil, true, version, fmt.Errorf("unsupported backup format version: %d", version)
	}
	salt := data[len(magicHeader)+1 : len(magicHeader)+1+16]
	nonce := data[len(magicHeader)+1+16 : headerLen]
	key := deriveKey(passphrase, salt, byte(version))
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, true, version, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, true, version, err
	}
	var aad []byte
	if version >= 2 {
		aad = data[:headerLen]
	}
	decrypted, err := aead.Open(nil, nonce, data[headerLen:], aad)
	if err != nil {
		return nil, true, version, errors.New("failed to decrypt backup: incorrect passphrase or corrupted data")
	}
	return decrypted, true, version, nil
}

func writeArchiveTarball(contents archiveContents) ([]byte, error) {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	files := []struct {
		name string
		body []byte
		mode int64
	}{
		{name: "state.json", body: contents.state, mode: 0o600},
		{name: "state.key", body: contents.key, mode: 0o600},
	}
	if len(contents.manifest) > 0 {
		files = append(files, struct {
			name string
			body []byte
			mode int64
		}{name: "manifest.json", body: contents.manifest, mode: 0o600})
	}
	for _, file := range files {
		header := &tar.Header{
			Name:     file.name,
			Mode:     file.mode,
			Size:     int64(len(file.body)),
			Typeflag: tar.TypeReg,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(file.body); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func readArchiveTarball(tarball []byte) (archiveContents, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return archiveContents{}, fmt.Errorf("initialize gzip reader: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	seen := make(map[string]bool)
	var contents archiveContents
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return archiveContents{}, fmt.Errorf("read tar archive: %w", err)
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		if name != "state.json" && name != "state.key" && name != "manifest.json" {
			return archiveContents{}, fmt.Errorf("invalid backup: unexpected archive entry %q", header.Name)
		}
		if seen[name] {
			return archiveContents{}, fmt.Errorf("invalid backup: duplicate archive entry %q", name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 {
			return archiveContents{}, fmt.Errorf("invalid backup: %q is not a regular file", name)
		}
		if header.Size < 0 || header.Size > maxBackupArchiveFileBytes {
			return archiveContents{}, fmt.Errorf("invalid backup: %q exceeds size limit", name)
		}
		body, err := io.ReadAll(io.LimitReader(reader, maxBackupArchiveFileBytes+1))
		if err != nil {
			return archiveContents{}, fmt.Errorf("read archive entry %q: %w", name, err)
		}
		if int64(len(body)) != header.Size {
			return archiveContents{}, fmt.Errorf("invalid backup: truncated archive entry %q", name)
		}
		seen[name] = true
		switch name {
		case "state.json":
			contents.state = body
		case "state.key":
			contents.key = body
		case "manifest.json":
			contents.manifest = body
		}
	}
	return contents, nil
}

func verifyManifestFiles(expected, actual []ArchiveFile) error {
	sort.Slice(expected, func(i, j int) bool { return expected[i].Name < expected[j].Name })
	sort.Slice(actual, func(i, j int) bool { return actual[i].Name < actual[j].Name })
	if len(expected) != len(actual) {
		return fmt.Errorf("backup manifest file count mismatch")
	}
	for i := range expected {
		if expected[i].Name != actual[i].Name ||
			expected[i].Size != actual[i].Size ||
			!strings.EqualFold(expected[i].SHA256, actual[i].SHA256) {
			return fmt.Errorf("backup checksum mismatch for %s", actual[i].Name)
		}
	}
	return nil
}

func validateStateAndKey(state, key []byte) error {
	if len(key) != secrets.KeySize {
		return fmt.Errorf("state.key length is %d bytes; expected %d", len(key), secrets.KeySize)
	}
	var keyArray [secrets.KeySize]byte
	copy(keyArray[:], key)
	cipher, err := secrets.NewCipher(keyArray)
	if err != nil {
		return err
	}
	snapshot, err := managementstate.NewManagementStateCodec().Decode(state)
	if err != nil {
		return err
	}
	if err := managementstate.DecryptSnapshot(&snapshot, cipher); err != nil {
		return fmt.Errorf("state and key do not match: %w", err)
	}
	return nil
}

func rawStateSchemaVersion(state []byte) int {
	var raw struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(state, &raw); err != nil || raw.SchemaVersion <= 0 {
		return 1
	}
	return raw.SchemaVersion
}

func backupChecksum(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

type stagedRestoreFile struct {
	target      string
	temp        string
	safety      string
	hadOriginal bool
	committed   bool
}

func stageRestoreFile(target string, body []byte, safety string) (*stagedRestoreFile, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".veil-restore-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(body); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return nil, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return nil, err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	_, statErr := os.Stat(target)
	return &stagedRestoreFile{
		target:      target,
		temp:        tempPath,
		safety:      safety,
		hadOriginal: statErr == nil,
	}, nil
}

func (f *stagedRestoreFile) commit() error {
	if f.hadOriginal {
		if err := os.Rename(f.target, f.safety); err != nil {
			return err
		}
	}
	if err := os.Rename(f.temp, f.target); err != nil {
		if f.hadOriginal {
			_ = os.Rename(f.safety, f.target)
		}
		return err
	}
	f.committed = true
	return nil
}

func (f *stagedRestoreFile) rollback() error {
	_ = os.Remove(f.temp)
	if f.committed {
		_ = os.Remove(f.target)
	}
	if f.hadOriginal {
		return os.Rename(f.safety, f.target)
	}
	return nil
}

func (f *stagedRestoreFile) cleanupStaged() error {
	return os.Remove(f.temp)
}
