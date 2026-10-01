package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
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
	"github.com/mikkelchokolate/Veil/internal/safefs"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

const (
	CurrentArchiveFormatVersion = 2
	LegacyArchiveFormatVersion  = 1
)

type ArchiveOptions struct {
	VeilVersion  string
	CreatedAt    time.Time
	DatabasePath string
	// MaxBytes is the explicit policy limit for both the published encrypted
	// archive and the total expanded members. Zero selects the production
	// default.
	MaxBytes int64
	// Crypto is scoped to this operation. Production callers leave it empty;
	// tests may provide a deterministic derivation without global state.
	Crypto CryptoOptions
	// afterStateCapture is a deterministic package-test hook. Production callers
	// leave it nil.
	afterStateCapture func()
}

type ArchiveFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ArchiveManifest struct {
	FormatVersion      int       `json:"formatVersion"`
	CreatedAt          time.Time `json:"createdAt"`
	VeilVersion        string    `json:"veilVersion"`
	StateSchemaVersion int       `json:"stateSchemaVersion"`
	// Pointer preserves verification compatibility with archive-v2 manifests
	// created before desiredRevision was recorded. New v2 archives always set
	// it, including revision zero.
	DesiredRevision *uint64       `json:"desiredRevision,omitempty"`
	Files           []ArchiveFile `json:"files"`
}

type VerificationReport struct {
	FormatVersion      int           `json:"formatVersion"`
	EncryptionVersion  int           `json:"encryptionVersion"`
	Encrypted          bool          `json:"encrypted"`
	Legacy             bool          `json:"legacy"`
	CreatedAt          time.Time     `json:"createdAt,omitempty"`
	VeilVersion        string        `json:"veilVersion,omitempty"`
	StateSchemaVersion int           `json:"stateSchemaVersion"`
	DesiredRevision    uint64        `json:"desiredRevision"`
	Files              []ArchiveFile `json:"files"`
}

type RestoreOptions struct {
	CheckOnly         bool
	Now               func() time.Time
	DatabasePath      string
	MaxBytes          int64
	FencingGeneration uint64
	// Crypto is scoped to this restore operation; empty selects production KDF.
	Crypto CryptoOptions
	// AllowUnencrypted is the explicit opt-in required to restore a
	// plaintext archive. Restores refuse plaintext by default; the
	// SFTP/remote boundary gates encryption independently of this flag
	// (#1223).
	AllowUnencrypted bool
}

// VerifyOptions carries the explicit policy for standalone verification.
type VerifyOptions struct {
	// MaxBytes is the explicit policy limit; zero selects the production
	// default.
	MaxBytes int64
	// Crypto is scoped to this verification; empty selects production KDF.
	Crypto CryptoOptions
	// AllowUnencrypted is the explicit opt-in required to verify a plaintext
	// archive; verification refuses plaintext by default (#1223).
	AllowUnencrypted bool
}

type RestoreResult struct {
	Verified           bool               `json:"verified"`
	CheckOnly          bool               `json:"checkOnly"`
	Verification       VerificationReport `json:"verification"`
	SafetyStatePath    string             `json:"safetyStatePath,omitempty"`
	SafetyKeyPath      string             `json:"safetyKeyPath,omitempty"`
	SafetyDatabasePath string             `json:"safetyDatabasePath,omitempty"`
}

type archiveContents struct {
	state    []byte
	key      []byte
	database []byte
	manifest []byte
}

type verifiedBackup struct {
	report   VerificationReport
	state    []byte
	key      []byte
	database []byte
}

func CreateBackupWithOptions(statePath, keyPath, passphrase string, options ArchiveOptions) ([]byte, error) {
	tarball, err := createTarballWithManifest(statePath, keyPath, options)
	if err != nil {
		return nil, err
	}
	return encryptBackupTarballWithOptions(tarball, passphrase, options.Crypto)
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
	if options.DatabasePath == "" {
		options.DatabasePath = filepath.Join(filepath.Dir(statePath), "veil.db")
	}
	if _, err := os.Stat(options.DatabasePath); err != nil {
		return nil, fmt.Errorf("archive database: %w", err)
	}
	database, desiredRevision, err := consistentSQLiteSnapshot(options.DatabasePath, backupChecksum(state))
	if err != nil {
		return nil, fmt.Errorf("archive database: %w", err)
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
		DesiredRevision:    &desiredRevision,
		Files: []ArchiveFile{
			{Name: "state.json", Size: int64(len(state)), SHA256: backupChecksum(state)},
			{Name: "state.key", Size: int64(len(key)), SHA256: backupChecksum(key)},
			{Name: "veil.db", Size: int64(len(database)), SHA256: backupChecksum(database)},
		},
	}
	manifestBody, err := archiveManifestMarshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal backup manifest: %w", err)
	}
	return writeArchiveTarball(archiveContents{state: state, key: key, database: database, manifest: manifestBody})
}

func consistentSQLiteSnapshot(databasePath, stateDigest string) ([]byte, uint64, error) {
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	tmp, err := os.CreateTemp(filepath.Dir(databasePath), ".veil-backup-db-*.sqlite")
	if err != nil {
		return nil, 0, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, 0, err
	}
	_ = os.Remove(tmpPath) // VACUUM INTO requires a non-existent target.
	defer os.Remove(tmpPath)
	quoted := "'" + strings.ReplaceAll(tmpPath, "'", "''") + "'"
	if _, err := db.Exec(`VACUUM INTO ` + quoted); err != nil {
		return nil, 0, err
	}
	desiredRevision, err := validateSQLiteDesiredSnapshotPath(tmpPath, nil, stateDigest)
	if err != nil {
		return nil, 0, err
	}
	body, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, 0, err
	}
	if len(body) == 0 {
		return nil, 0, errors.New("SQLite snapshot is empty")
	}
	return body, desiredRevision, nil
}

func VerifyBackup(data []byte, passphrase string) (VerificationReport, error) {
	return VerifyBackupWithOptions(data, passphrase, CryptoOptions{})
}

func VerifyBackupWithOptions(data []byte, passphrase string, crypto CryptoOptions) (VerificationReport, error) {
	verified, err := inspectBackupWithOptions(data, passphrase, crypto)
	if err != nil {
		return VerificationReport{}, err
	}
	return verified.report, nil
}

// RestoreBackupWithOptions restores a caller-supplied archive byte slice. The
// bytes are staged to a bounded temporary file and routed through the same
// journaled file-based path as RestoreBackupFileWithOptions: durable journal +
// interrupted-restore recovery + snapshot barrier, instead of the unjournalled
// stagedRestoreFile.commit path that left a torn triple plus unrecoverable
// .pre-restore-* files on a crash (#1124). The MaxBytes policy bounds the
// caller-supplied input before it is staged.
func RestoreBackupWithOptions(data []byte, statePath, keyPath, passphrase string, options RestoreOptions) (RestoreResult, error) {
	maxBytes, err := normalizeBackupMaxBytes(options.MaxBytes)
	if err != nil {
		return RestoreResult{}, err
	}
	if int64(len(data)) > maxBytes {
		return RestoreResult{}, backupPolicyError(int64(len(data)), maxBytes)
	}
	workDir, err := os.MkdirTemp("", "veil-restore-archive-*")
	if err != nil {
		return RestoreResult{}, err
	}
	defer os.RemoveAll(workDir)
	archivePath := filepath.Join(workDir, "archive.tar.gz")
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		return RestoreResult{}, err
	}
	return RestoreBackupFileWithOptions(archivePath, statePath, keyPath, passphrase, options)
}

func inspectBackup(data []byte, passphrase string) (verifiedBackup, error) {
	return inspectBackupWithOptions(data, passphrase, CryptoOptions{})
}

// inspectBackupWithOptions stages caller-supplied archive bytes to a bounded
// temp file and routes them through the same streaming verification as
// inspectBackupFileWithOptions — one implementation keeps manifest, schema,
// database and size-policy checks identical between the byte and file APIs
// (the separate in-memory path was dead divergent code, #1223). The byte API
// keeps accepting plaintext archives: the require-encryption boundary lives
// on the file restore/verify path (VerifyOptions/RestoreOptions).
func inspectBackupWithOptions(data []byte, passphrase string, options CryptoOptions) (verifiedBackup, error) {
	maxBytes, err := normalizeBackupMaxBytes(0)
	if err != nil {
		return verifiedBackup{}, err
	}
	if int64(len(data)) > maxBytes {
		return verifiedBackup{}, backupPolicyError(int64(len(data)), maxBytes)
	}
	workDir, err := os.MkdirTemp("", "veil-verify-archive-*")
	if err != nil {
		return verifiedBackup{}, err
	}
	defer os.RemoveAll(workDir)
	archivePath := filepath.Join(workDir, "archive.tar.gz")
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		return verifiedBackup{}, err
	}
	extracted, err := inspectBackupFileWithOptions(archivePath, passphrase, maxBytes, options, true)
	if err != nil {
		return verifiedBackup{}, err
	}
	defer extracted.cleanup()
	verified := verifiedBackup{report: extracted.report, state: extracted.state, key: extracted.key}
	if extracted.databasePath != "" {
		verified.database, err = os.ReadFile(extracted.databasePath)
		if err != nil {
			return verifiedBackup{}, err
		}
	}
	return verified, nil
}

func checkpointSQLiteRestoreBoundary(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	db, err := storage.OpenExisting(path)
	if err != nil {
		return err
	}
	var busy, logFrames, checkpointed int
	err = db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed)
	closeErr := db.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if busy != 0 {
		return fmt.Errorf("veil.db is still in use (WAL frames=%d checkpointed=%d); stop writers before restore", logFrames, checkpointed)
	}
	return nil
}

func validateSQLiteDesiredSnapshotPath(path string, expectedDesiredRevision *uint64, expectedStateDigest string) (uint64, error) {
	db, err := storage.OpenExisting(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return validateSQLiteDesiredSnapshotDB(db, expectedDesiredRevision, expectedStateDigest)
}

func validateSQLiteDesiredSnapshotDB(db interface {
	QueryRow(query string, args ...any) *sql.Row
}, expectedDesiredRevision *uint64, expectedStateDigest string) (uint64, error) {
	var desiredRevision uint64
	if err := db.QueryRow(`SELECT desired_revision FROM revisions WHERE id=1`).Scan(&desiredRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if expectedDesiredRevision != nil && *expectedDesiredRevision != 0 {
				return 0, fmt.Errorf("captured desired revision mismatch: manifest=%d database=0", *expectedDesiredRevision)
			}
			return 0, nil
		}
		return 0, fmt.Errorf("read captured desired revision: %w", err)
	}
	if expectedDesiredRevision != nil && desiredRevision != *expectedDesiredRevision {
		return 0, fmt.Errorf("captured desired revision mismatch: manifest=%d database=%d", *expectedDesiredRevision, desiredRevision)
	}
	if desiredRevision == 0 {
		return desiredRevision, nil
	}
	var digestColumnCount int
	if err := db.QueryRow(
		`SELECT COUNT(1) FROM pragma_table_info('revision_snapshots') WHERE name='state_sha256'`,
	).Scan(&digestColumnCount); err != nil {
		return 0, fmt.Errorf("inspect immutable snapshot state digest schema: %w", err)
	}
	if digestColumnCount == 0 {
		return 0, errors.New("immutable snapshots do not support state digest binding")
	}
	var stateDigest string
	if err := db.QueryRow(`SELECT state_sha256 FROM revision_snapshots WHERE revision=?`, desiredRevision).Scan(&stateDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("immutable snapshot for desired revision %d is missing", desiredRevision)
		}
		return 0, fmt.Errorf("verify immutable snapshot for desired revision %d: %w", desiredRevision, err)
	}
	if len(stateDigest) != sha256.Size*2 {
		return 0, fmt.Errorf("immutable snapshot state digest for desired revision %d is missing or invalid", desiredRevision)
	}
	if _, err := hex.DecodeString(stateDigest); err != nil {
		return 0, fmt.Errorf("immutable snapshot state digest for desired revision %d is invalid", desiredRevision)
	}
	if expectedStateDigest != "" && !strings.EqualFold(stateDigest, expectedStateDigest) {
		return 0, fmt.Errorf("state digest mismatch for desired revision %d: snapshot=%s archive=%s", desiredRevision, stateDigest, expectedStateDigest)
	}
	return desiredRevision, nil
}

func decryptBackup(data []byte, passphrase string) ([]byte, bool, int, error) {
	return decryptBackupWithOptions(data, passphrase, CryptoOptions{})
}

func decryptBackupWithOptions(data []byte, passphrase string, options CryptoOptions) ([]byte, bool, int, error) {
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
	key := deriveKeyWithOptions(passphrase, salt, byte(version), options)
	block, err := decryptAESNewCipher(key)
	if err != nil {
		return nil, true, version, err
	}
	aead, err := decryptNewGCM(block)
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
	var files []struct {
		name string
		body []byte
		mode int64
	}
	// Members are written only when present, so a nil body produces a
	// genuinely absent member that verification reports as missing rather
	// than a zero-length file that fails state validation downstream.
	if len(contents.state) > 0 {
		files = append(files, struct {
			name string
			body []byte
			mode int64
		}{name: "state.json", body: contents.state, mode: 0o600})
	}
	if len(contents.key) > 0 {
		files = append(files, struct {
			name string
			body []byte
			mode int64
		}{name: "state.key", body: contents.key, mode: 0o600})
	}
	if len(contents.database) > 0 {
		files = append(files, struct {
			name string
			body []byte
			mode int64
		}{name: "veil.db", body: contents.database, mode: 0o600})
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
		if err := archiveWriteHeader(tarWriter, header); err != nil {
			return nil, err
		}
		if _, err := archiveWrite(tarWriter, file.body); err != nil {
			return nil, err
		}
	}
	if err := archiveClose(tarWriter); err != nil {
		return nil, err
	}
	if err := archiveGzipClose(gzipWriter); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func readArchiveTarball(tarball []byte) (archiveContents, error) {
	return readArchiveTarballWithMax(tarball, DefaultMaxBackupBytes)
}

func readArchiveTarballWithMax(tarball []byte, maxBytes int64) (archiveContents, error) {
	if maxBytes <= 0 {
		return archiveContents{}, errors.New("backup size policy must be positive")
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return archiveContents{}, fmt.Errorf("initialize gzip reader: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	seen := make(map[string]bool)
	var contents archiveContents
	var expandedBytes int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return archiveContents{}, fmt.Errorf("read tar archive: %w", err)
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		if name != "state.json" && name != "state.key" && name != "veil.db" && name != "manifest.json" {
			return archiveContents{}, fmt.Errorf("invalid backup: unexpected archive entry %q", header.Name)
		}
		if seen[name] {
			return archiveContents{}, fmt.Errorf("invalid backup: duplicate archive entry %q", name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 {
			return archiveContents{}, fmt.Errorf("invalid backup: %q is not a regular file", name)
		}
		if header.Size < 0 || header.Size > maxBytes {
			return archiveContents{}, fmt.Errorf("invalid backup: %q exceeds size limit", name)
		}
		// Bound aggregate expanded bytes, not just each member: per-member
		// limits alone let a crafted multi-member tarball expand far beyond the
		// configured policy in memory (#1124).
		if expandedBytes, err = addPolicyBytes(expandedBytes, header.Size, maxBytes); err != nil {
			return archiveContents{}, err
		}
		body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
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
		case "veil.db":
			contents.database = body
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
	cipher, err := validateSecretsNewCipher(keyArray)
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

// Filesystem hooks for restore staging; overridable in tests to inject failures.
// Metadata and mutation operations run on open descriptors or descriptor-
// relative directory handles — never on a re-resolved path under the
// service-writable state directory, where a leaf swapped for a symlink would
// redirect a root-side chmod/chown/rename onto an attacker target (#1219).
var (
	restoreMkdirAll   = os.MkdirAll
	restoreCreateTemp = os.CreateTemp
	// restoreRename is descriptor-relative: (dir, oldLeaf, newLeaf).
	restoreRename = func(dir *safefs.Dir, oldLeaf, newLeaf string) error {
		return dir.RenameAt(oldLeaf, newLeaf)
	}
	// restoreRemove is descriptor-relative: (dir, leaf).
	restoreRemove = func(dir *safefs.Dir, leaf string) error {
		return dir.RemoveAt(leaf)
	}
	// restoreRemovePath removes a staging temp by full path. Only ever used on
	// a fresh .veil-restore-* leaf this process created, where path removal is
	// safe: a swapped leaf just unlinks whatever name is present.
	restoreRemovePath = os.Remove

	restoreFileWrite = (*os.File).Write
	restoreFileSync  = (*os.File).Sync
	restoreFileClose = (*os.File).Close
	restoreFileChmod = (*os.File).Chmod
)

// Archive and crypto hooks overridable in tests.
var (
	archiveManifestMarshal = json.Marshal
	archiveWriteHeader     = (*tar.Writer).WriteHeader
	archiveWrite           = (*tar.Writer).Write
	archiveClose           = (*tar.Writer).Close
	archiveGzipClose       = (*gzip.Writer).Close

	decryptAESNewCipher = aes.NewCipher
	decryptNewGCM       = cipher.NewGCM

	validateSecretsNewCipher = secrets.NewCipher
)

type stagedRestoreFile struct {
	target      string
	temp        string
	safety      string
	hadOriginal bool
	committed   bool
	// Metadata captured while staging, all observed on one pinned observation
	// of the target: intendedDigest is the sha256 of the staged bytes and
	// previousDigest/mode/uid/gid describe the pre-restore target. The journal
	// records these values directly so recovery never has to trust — or
	// re-read by name — attacker-mutable fields (#1219).
	intendedDigest string
	previousDigest string
	mode           os.FileMode
	uid            int
	gid            int
}

// restoreTargetObservation is one coherent, descriptor-pinned view of the
// pre-restore target: the leaf is opened O_NOFOLLOW and fstat-verified, so
// the mode/owner/digest recorded here cannot be redirected by a mid-flight
// name swap (#1219).
type restoreTargetObservation struct {
	mode   os.FileMode
	uid    int
	gid    int
	digest string
}

func observeRestoreTarget(target string) (*restoreTargetObservation, error) {
	// An explicit lstat first preserves the clear "not a regular file"
	// rejection for a statically planted symlink; the descriptor-pinned open
	// below still guards the swap window after this check.
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("restore target %s is not a regular file", target)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else {
		return nil, err
	}
	file, err := safefs.OpenNoFollow(target)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("restore target %s is not a regular file", target)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	observation := &restoreTargetObservation{
		mode:   info.Mode().Perm(),
		digest: hex.EncodeToString(hash.Sum(nil)),
	}
	observation.uid, observation.gid = fileOwnerIDs(info)
	return observation, nil
}

// finishRestoreStaging applies mode/ownership to the open temp descriptor —
// while the inode is still pinned — then syncs and closes it. The .veil-*
// temp name is never used for a metadata operation again (#1219).
func finishRestoreStaging(temp *os.File, tempPath, target, safety, intendedDigest string) (*stagedRestoreFile, error) {
	dirPath := filepath.Dir(target)
	fail := func(err error) (*stagedRestoreFile, error) {
		_ = restoreFileClose(temp)
		_ = restoreRemovePath(tempPath)
		return nil, err
	}
	observed, err := observeRestoreTarget(target)
	if err != nil {
		return fail(err)
	}
	// Preserve the original file's mode and ownership: the restore may run as
	// root (privileged helper) while the panel process runs unprivileged. A
	// replacement written root-owned 0600 would leave the panel unable to
	// re-read its own state/key after restore (the reload step then fails and
	// the whole restore job is reported as failed despite state on disk being
	// correct).
	mode := os.FileMode(0o600)
	if observed != nil {
		mode = observed.mode
	}
	if err := restoreFileChmod(temp, mode); err != nil {
		return fail(err)
	}
	if observed != nil {
		if err := restoreChownToMatch(temp, observed.uid, observed.gid); err != nil {
			return fail(err)
		}
	}
	if err := restoreFileSync(temp); err != nil {
		return fail(err)
	}
	if err := restoreFileClose(temp); err != nil {
		_ = restoreRemovePath(tempPath)
		return nil, err
	}
	if err := syncDirectory(dirPath); err != nil {
		_ = restoreRemovePath(tempPath)
		return nil, err
	}
	staged := &stagedRestoreFile{
		target:         target,
		temp:           tempPath,
		safety:         safety,
		mode:           mode,
		intendedDigest: intendedDigest,
	}
	if observed != nil {
		staged.hadOriginal = true
		staged.previousDigest = observed.digest
		staged.uid = observed.uid
		staged.gid = observed.gid
	}
	return staged, nil
}

func stageRestoreFile(target string, body []byte, safety string) (*stagedRestoreFile, error) {
	dirPath := filepath.Dir(target)
	if err := restoreMkdirAll(dirPath, 0o700); err != nil {
		return nil, err
	}
	temp, err := restoreCreateTemp(dirPath, ".veil-restore-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	if _, err := restoreFileWrite(temp, body); err != nil {
		_ = restoreFileClose(temp)
		_ = restoreRemovePath(tempPath)
		return nil, err
	}
	return finishRestoreStaging(temp, tempPath, target, safety, backupChecksum(body))
}

func (f *stagedRestoreFile) pinnedTargetDir() (*safefs.Dir, error) {
	return safefs.OpenDir(filepath.Dir(f.target))
}

// commit performs the safety swap with descriptor-relative renames: the
// staged and safety leaves are moved inside the pinned target directory, so
// a leaf swapped mid-flight is renamed as its own inode — never followed —
// and a planted symlink simply moves where a regular rename of it would
// land instead of tricking a path-based chmod/chown later (#1219).
func (f *stagedRestoreFile) commit() error {
	dir, err := f.pinnedTargetDir()
	if err != nil {
		return err
	}
	defer dir.Close()
	if f.hadOriginal {
		if err := restoreRename(dir, filepath.Base(f.target), filepath.Base(f.safety)); err != nil {
			return err
		}
	}
	if err := restoreRename(dir, filepath.Base(f.temp), filepath.Base(f.target)); err != nil {
		if f.hadOriginal {
			_ = restoreRename(dir, filepath.Base(f.safety), filepath.Base(f.target))
		}
		return err
	}
	f.committed = true
	return nil
}

func (f *stagedRestoreFile) rollback() error {
	dir, err := f.pinnedTargetDir()
	if err != nil {
		return err
	}
	defer dir.Close()
	_ = restoreRemove(dir, filepath.Base(f.temp))
	if f.committed {
		_ = restoreRemove(dir, filepath.Base(f.target))
	}
	if f.hadOriginal {
		return restoreRename(dir, filepath.Base(f.safety), filepath.Base(f.target))
	}
	return nil
}

func (f *stagedRestoreFile) cleanupStaged() error {
	return restoreRemovePath(f.temp)
}
