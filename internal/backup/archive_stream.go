package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

const (
	// DefaultMaxBackupBytes is an explicit production policy, not a format
	// limitation. Operators can lower or raise it through the privileged
	// executor's VEIL_BACKUP_MAX_BYTES setting.
	DefaultMaxBackupBytes    int64 = 16 * 1024 * 1024 * 1024
	backupChunkBytes               = 1024 * 1024
	chunkedEncryptionVersion       = byte(3)
)

var backupStatfs = syscall.Statfs

func preflightBackupSpace(destinationDir string, sourcePaths []string, maxBytes int64) error {
	const safetyReserve = int64(64 * 1024 * 1024)
	var sourceBytes int64
	for _, sourcePath := range sourcePaths {
		info, err := os.Stat(sourcePath)
		if err != nil {
			return fmt.Errorf("backup space preflight stat %s: %w", sourcePath, err)
		}
		if info.Size() < 0 || info.Size() > maxBytes || sourceBytes > math.MaxInt64-info.Size() {
			return fmt.Errorf("source exceeds configured backup size policy")
		}
		sourceBytes += info.Size()
	}
	if sourceBytes > (math.MaxInt64-safetyReserve)/2 {
		return fmt.Errorf("backup space estimate overflow")
	}
	required := sourceBytes*2 + safetyReserve
	var stats syscall.Statfs_t
	if err := backupStatfs(destinationDir, &stats); err != nil {
		return fmt.Errorf("backup space preflight: %w", err)
	}
	available := uint64(stats.Bavail) * uint64(stats.Bsize)
	if uint64(required) > available {
		return fmt.Errorf("insufficient free space for backup: require %d bytes, available %d", required, available)
	}
	return nil
}

func PreflightVerifySpace(archivePath string, maxBytes int64) error {
	return preflightBackupOperationSpace(archivePath, "", "", "", maxBytes, false)
}

func PreflightRestoreSpace(archivePath, statePath, keyPath, databasePath string, maxBytes int64) error {
	return preflightBackupOperationSpace(archivePath, statePath, keyPath, databasePath, maxBytes, true)
}

func preflightBackupOperationSpace(archivePath, statePath, keyPath, databasePath string, configuredMax int64, restoring bool) error {
	maxBytes, err := normalizeBackupMaxBytes(configuredMax)
	if err != nil {
		return err
	}
	archiveInfo, err := os.Lstat(archivePath)
	if err != nil {
		return fmt.Errorf("backup space preflight archive: %w", err)
	}
	if !archiveInfo.Mode().IsRegular() || archiveInfo.Size() < 0 || archiveInfo.Size() > maxBytes {
		return backupPolicyError(archiveInfo.Size(), maxBytes)
	}
	requirements := map[string][]int64{
		filepath.Dir(archivePath): {archiveInfo.Size(), maxBytes},
	}
	if restoring {
		stateDir := filepath.Dir(statePath)
		components := []int64{maxBytes}
		for _, currentPath := range []string{statePath, keyPath, databasePath} {
			if currentPath == "" {
				continue
			}
			info, statErr := os.Stat(currentPath)
			if os.IsNotExist(statErr) {
				continue
			}
			if statErr != nil {
				return fmt.Errorf("restore space preflight stat %s: %w", currentPath, statErr)
			}
			if !info.Mode().IsRegular() || info.Size() < 0 {
				return fmt.Errorf("restore space preflight source is not a regular file")
			}
			components = append(components, info.Size())
		}
		requirements[stateDir] = append(requirements[stateDir], components...)
	}
	return requireBackupSpaceByFilesystem(requirements)
}

func requireBackupSpaceByFilesystem(requirements map[string][]int64) error {
	const safetyReserve = int64(64 * 1024 * 1024)
	type filesystemNeed struct {
		required  int64
		available uint64
	}
	needs := make(map[string]filesystemNeed)
	for directory, components := range requirements {
		var stats syscall.Statfs_t
		if err := backupStatfs(directory, &stats); err != nil {
			return fmt.Errorf("backup operation space preflight: %w", err)
		}
		key := fmt.Sprintf("%v", stats.Fsid)
		need := needs[key]
		if need.available == 0 {
			need.available = uint64(stats.Bavail) * uint64(stats.Bsize)
		}
		for _, component := range components {
			if component < 0 || need.required > math.MaxInt64-component {
				return fmt.Errorf("backup operation space estimate overflow")
			}
			need.required += component
		}
		needs[key] = need
	}
	for key, need := range needs {
		if need.required > math.MaxInt64-safetyReserve {
			return fmt.Errorf("backup operation space estimate overflow")
		}
		need.required += safetyReserve
		if uint64(need.required) > need.available {
			return fmt.Errorf("insufficient free space for backup operation on filesystem %s: require %d bytes, available %d", key, need.required, need.available)
		}
	}
	return nil
}

// ConfiguredMaxBackupBytes resolves the explicit production backup policy.
// VEIL_BACKUP_MAX_BYTES is a positive byte count; when unset the default is
// 16 GiB. The policy applies to both encrypted bytes and expanded members.
func ConfiguredMaxBackupBytes() (int64, error) {
	raw := strings.TrimSpace(os.Getenv("VEIL_BACKUP_MAX_BYTES"))
	if raw == "" {
		return DefaultMaxBackupBytes, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("VEIL_BACKUP_MAX_BYTES must be a positive integer byte count")
	}
	return value, nil
}

type streamArchiveFile struct {
	name string
	path string
	meta ArchiveFile
	mode int64
}

type extractedBackup struct {
	workDir      string
	report       VerificationReport
	state        []byte
	key          []byte
	databasePath string
}

func (v *extractedBackup) cleanup() { _ = os.RemoveAll(v.workDir) }

// CreateBackupFileWithOptions writes a backup directly to destination. New
// encrypted file backups use independently authenticated 1 MiB frames, so the
// tar/gzip stream never needs to be materialized in RAM. The final archive is
// atomically renamed into place only after every layer has closed and synced.
func CreateBackupFileWithOptions(destination, statePath, keyPath, passphrase string, options ArchiveOptions) error {
	return createBackupFileWithOptionsUnlocked(destination, statePath, keyPath, passphrase, options)
}

func createBackupFileWithOptionsUnlocked(destination, statePath, keyPath, passphrase string, options ArchiveOptions) (retErr error) {
	maxBytes, err := normalizeBackupMaxBytes(options.MaxBytes)
	if err != nil {
		return err
	}
	if strings.TrimSpace(destination) == "" {
		return errors.New("backup destination is required")
	}
	if options.DatabasePath == "" {
		options.DatabasePath = filepath.Join(filepath.Dir(statePath), "veil.db")
	}
	destinationDir := filepath.Dir(destination)
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if err := preflightBackupSpace(destinationDir, []string{statePath, keyPath, options.DatabasePath}, maxBytes); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp(destinationDir, ".veil-backup-create-*")
	if err != nil {
		return fmt.Errorf("create backup workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Hold the cross-process mutation barrier only while both domains are
	// captured. Tar/gzip/encryption stream from these private files after unlock.
	var stateSnapshot, keySnapshot, databaseSnapshot string
	var desiredRevision uint64
	var stateDigest string
	err = managementstate.WithSnapshotBarrier(statePath, func() error {
		stateSnapshot, err = copySnapshotFile(statePath, filepath.Join(workDir, "state.json"), maxBytes)
		if err != nil {
			return fmt.Errorf("capture state snapshot: %w", err)
		}
		stateMetadata, err := archiveFileMetadata("state.json", stateSnapshot)
		if err != nil {
			return fmt.Errorf("digest captured state snapshot: %w", err)
		}
		stateDigest = stateMetadata.SHA256
		if options.afterStateCapture != nil {
			options.afterStateCapture()
		}
		keySnapshot, err = copySnapshotFile(keyPath, filepath.Join(workDir, "state.key"), maxBytes)
		if err != nil {
			return fmt.Errorf("archive key: %w", err)
		}
		databaseSnapshot, desiredRevision, err = consistentSQLiteSnapshotFile(options.DatabasePath, workDir, stateDigest)
		if err != nil {
			return fmt.Errorf("archive database: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	files := []streamArchiveFile{
		{name: "state.json", path: stateSnapshot, mode: 0o600},
		{name: "state.key", path: keySnapshot, mode: 0o600},
		{name: "veil.db", path: databaseSnapshot, mode: 0o600},
	}
	var expandedBytes int64
	for i := range files {
		files[i].meta, err = archiveFileMetadata(files[i].name, files[i].path)
		if err != nil {
			return err
		}
		expandedBytes, err = addPolicyBytes(expandedBytes, files[i].meta.Size, maxBytes)
		if err != nil {
			return err
		}
	}
	stateSchemaVersion, err := rawStateSchemaVersionFile(stateSnapshot)
	if err != nil {
		return fmt.Errorf("read state schema version: %w", err)
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
		StateSchemaVersion: stateSchemaVersion,
		DesiredRevision:    &desiredRevision,
		Files:              []ArchiveFile{files[0].meta, files[1].meta, files[2].meta},
	}
	manifestBody, err := archiveManifestMarshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal backup manifest: %w", err)
	}
	expandedBytes, err = addPolicyBytes(expandedBytes, int64(len(manifestBody)), maxBytes)
	if err != nil {
		return err
	}
	_ = expandedBytes
	manifestPath := filepath.Join(workDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBody, 0o600); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}
	files = append(files, streamArchiveFile{name: "manifest.json", path: manifestPath, mode: 0o600})

	temp, err := os.CreateTemp(destinationDir, ".veil-backup-publish-*")
	if err != nil {
		return fmt.Errorf("create backup output: %w", err)
	}
	tempPath := temp.Name()
	published := false
	defer func() {
		if !published {
			_ = temp.Close()
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	limitedOutput := &backupPolicyWriter{writer: temp, maxBytes: maxBytes}
	var archiveWriter io.Writer = limitedOutput
	var encryptWriter *chunkEncryptWriter
	if passphrase != "" {
		encryptWriter, err = newChunkEncryptWriter(limitedOutput, passphrase)
		if err != nil {
			return err
		}
		archiveWriter = encryptWriter
	}
	gzipWriter := gzip.NewWriter(archiveWriter)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		if err := writeTarFileStream(tarWriter, file); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close backup tar: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close backup gzip: %w", err)
	}
	if encryptWriter != nil {
		if err := encryptWriter.Close(); err != nil {
			return fmt.Errorf("close backup encryption: %w", err)
		}
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync backup archive: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close backup archive: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("publish backup archive: %w", err)
	}
	published = true
	if err := syncDirectory(destinationDir); err != nil {
		return fmt.Errorf("sync backup directory: %w", err)
	}
	return nil
}

func VerifyBackupFile(path, passphrase string, maxBytes int64) (VerificationReport, error) {
	verified, err := inspectBackupFile(path, passphrase, maxBytes)
	if err != nil {
		return VerificationReport{}, err
	}
	defer verified.cleanup()
	return verified.report, nil
}

func RestoreBackupFileWithOptions(archivePath, statePath, keyPath, passphrase string, options RestoreOptions) (RestoreResult, error) {
	if options.DatabasePath == "" && statePath != "" {
		options.DatabasePath = filepath.Join(filepath.Dir(statePath), "veil.db")
	}
	if err := RecoverInterruptedRestore(statePath, keyPath, options.DatabasePath); err != nil {
		return RestoreResult{}, fmt.Errorf("recover interrupted restore: %w", err)
	}
	verified, err := inspectBackupFile(archivePath, passphrase, options.MaxBytes)
	if err != nil {
		return RestoreResult{}, err
	}
	defer verified.cleanup()
	result := RestoreResult{Verified: true, CheckOnly: options.CheckOnly, Verification: verified.report}
	if options.CheckOnly {
		return result, nil
	}
	if verified.databasePath != "" {
		if err := checkpointSQLiteRestoreBoundary(options.DatabasePath); err != nil {
			return RestoreResult{}, fmt.Errorf("prepare database restore boundary: %w", err)
		}
	}
	previousRevision, err := readRestoreRevision(options.DatabasePath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("read previous restore revision: %w", err)
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
		_ = stateBackup.cleanupStaged()
		return RestoreResult{}, fmt.Errorf("stage key restore: %w", err)
	}
	staged := []*stagedRestoreFile{stateBackup, keyBackup}
	names := []string{"state.json", "state.key"}
	var databaseBackup *stagedRestoreFile
	databaseSafety := ""
	if verified.databasePath != "" {
		databaseSafety = options.DatabasePath + ".pre-restore-" + suffix
		databaseBackup, err = stageRestoreFileFromPath(options.DatabasePath, verified.databasePath, databaseSafety)
		if err != nil {
			_ = stateBackup.cleanupStaged()
			_ = keyBackup.cleanupStaged()
			return RestoreResult{}, fmt.Errorf("stage database restore: %w", err)
		}
		staged = append(staged, databaseBackup)
		names = append(names, "veil.db")
	}
	journal, err := prepareRestoreJournalFenced(statePath, staged, names, previousRevision, verified.report.DesiredRevision, options.FencingGeneration)
	if err != nil {
		for _, file := range staged {
			_ = file.cleanupStaged()
		}
		return RestoreResult{}, fmt.Errorf("prepare durable restore journal: %w", err)
	}
	root := filepath.Dir(statePath)
	for index := range staged {
		if err := publishRestoreJournalFile(root, &journal, index); err != nil {
			if rollbackErr := rollbackRestoreJournal(root, &journal); rollbackErr != nil {
				return RestoreResult{}, fmt.Errorf("replace backup member %d: %v; rollback: %w", index, err, rollbackErr)
			}
			return RestoreResult{}, fmt.Errorf("replace backup member %d: %w", index, err)
		}
	}
	if err := completeRestoreJournal(root, func() string {
		if databaseBackup != nil {
			return options.DatabasePath
		}
		return ""
	}(), &journal); err != nil {
		if errors.Is(err, errRestoreCommitted) {
			// The exact intended set, revision binding and fencing floor are
			// already durable. Leave the marker for idempotent startup cleanup;
			// never roll a committed restore back because unlink failed.
			goto restoreCommitted
		}
		if rollbackErr := rollbackRestoreJournal(root, &journal); rollbackErr != nil {
			return RestoreResult{}, fmt.Errorf("finalize restored backup: %v; rollback: %w", err, rollbackErr)
		}
		return RestoreResult{}, fmt.Errorf("finalize restored backup: %w", err)
	}

restoreCommitted:
	if stateBackup.hadOriginal {
		result.SafetyStatePath = stateSafety
	}
	if keyBackup.hadOriginal {
		result.SafetyKeyPath = keySafety
	}
	if databaseBackup != nil && databaseBackup.hadOriginal {
		result.SafetyDatabasePath = databaseSafety
	}
	return result, nil
}

func inspectBackupFile(path, passphrase string, configuredMax int64) (*extractedBackup, error) {
	maxBytes, err := normalizeBackupMaxBytes(configuredMax)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("backup archive is not a regular file")
	}
	if info.Size() < 0 || info.Size() > maxBytes {
		return nil, backupPolicyError(info.Size(), maxBytes)
	}
	workDir, err := os.MkdirTemp(filepath.Dir(path), ".veil-backup-inspect-*")
	if err != nil {
		return nil, fmt.Errorf("create backup inspection workspace: %w", err)
	}
	result := &extractedBackup{workDir: workDir}
	ok := false
	defer func() {
		if !ok {
			result.cleanup()
		}
	}()
	tarballPath, encrypted, encryptionVersion, err := prepareTarballFile(path, passphrase, maxBytes, workDir)
	if err != nil {
		return nil, err
	}
	if err := extractAndVerifyTarball(tarballPath, maxBytes, result); err != nil {
		return nil, err
	}
	result.report.Encrypted = encrypted
	result.report.EncryptionVersion = encryptionVersion
	ok = true
	return result, nil
}

func extractAndVerifyTarball(tarballPath string, maxBytes int64, result *extractedBackup) error {
	file, err := openBackupRegularNoFollow(tarballPath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("initialize gzip reader: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	seen := make(map[string]bool)
	metadata := make(map[string]ArchiveFile)
	paths := make(map[string]string)
	var expandedBytes int64
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		if name != "state.json" && name != "state.key" && name != "veil.db" && name != "manifest.json" {
			return fmt.Errorf("invalid backup: unexpected archive entry %q", header.Name)
		}
		if seen[name] {
			return fmt.Errorf("invalid backup: duplicate archive entry %q", name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 {
			return fmt.Errorf("invalid backup: %q is not a regular file", name)
		}
		if header.Size < 0 {
			return fmt.Errorf("invalid backup: negative size for %q", name)
		}
		expandedBytes, err = addPolicyBytes(expandedBytes, header.Size, maxBytes)
		if err != nil {
			return err
		}
		destination := filepath.Join(result.workDir, "member-"+strings.ReplaceAll(name, ".", "-"))
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		hash := sha256.New()
		written, copyErr := io.CopyN(io.MultiWriter(output, hash), tarReader, header.Size)
		closeErr := output.Close()
		if copyErr != nil {
			return fmt.Errorf("read archive entry %q: %w", name, copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if written != header.Size {
			return fmt.Errorf("invalid backup: truncated archive entry %q", name)
		}
		seen[name] = true
		paths[name] = destination
		metadata[name] = ArchiveFile{Name: name, Size: header.Size, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	if !seen["state.json"] {
		return errors.New("invalid backup: missing state.json")
	}
	if !seen["state.key"] {
		return errors.New("invalid backup: missing state.key")
	}
	result.state, err = os.ReadFile(paths["state.json"])
	if err != nil {
		return err
	}
	result.key, err = os.ReadFile(paths["state.key"])
	if err != nil {
		return err
	}
	sourceSchema := rawStateSchemaVersion(result.state)
	if sourceSchema > managementstate.CurrentSchemaVersion {
		return fmt.Errorf("backup uses newer state schema %d; this Veil supports up to %d", sourceSchema, managementstate.CurrentSchemaVersion)
	}
	if err := validateStateAndKey(result.state, result.key); err != nil {
		return fmt.Errorf("validate backup state: %w", err)
	}
	result.report = VerificationReport{
		Legacy:             !seen["manifest.json"],
		StateSchemaVersion: sourceSchema,
		Files:              []ArchiveFile{metadata["state.json"], metadata["state.key"]},
	}
	if !seen["manifest.json"] {
		return nil
	}
	manifestBody, err := os.ReadFile(paths["manifest.json"])
	if err != nil {
		return err
	}
	var manifest ArchiveManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return fmt.Errorf("decode backup manifest: %w", err)
	}
	if manifest.FormatVersion != LegacyArchiveFormatVersion && manifest.FormatVersion != CurrentArchiveFormatVersion {
		return fmt.Errorf("unsupported archive manifest version: %d", manifest.FormatVersion)
	}
	if manifest.FormatVersion == CurrentArchiveFormatVersion {
		if !seen["veil.db"] {
			return errors.New("invalid backup: missing veil.db")
		}
		result.databasePath = paths["veil.db"]
		result.report.Files = append(result.report.Files, metadata["veil.db"])
		if err := validateSQLiteSnapshotFile(result.databasePath, manifest.DesiredRevision, metadata["state.json"].SHA256); err != nil {
			return fmt.Errorf("validate backup database: %w", err)
		}
	}
	if manifest.StateSchemaVersion != sourceSchema {
		return fmt.Errorf("backup manifest state schema mismatch: manifest=%d archive=%d", manifest.StateSchemaVersion, sourceSchema)
	}
	if err := verifyManifestFiles(manifest.Files, result.report.Files); err != nil {
		return err
	}
	result.report.FormatVersion = manifest.FormatVersion
	result.report.CreatedAt = manifest.CreatedAt.UTC()
	result.report.VeilVersion = manifest.VeilVersion
	result.report.Files = manifest.Files
	if manifest.DesiredRevision != nil {
		result.report.DesiredRevision = *manifest.DesiredRevision
	}
	return nil
}

func prepareTarballFile(path, passphrase string, maxBytes int64, workDir string) (string, bool, int, error) {
	input, err := openBackupRegularNoFollow(path)
	if err != nil {
		return "", false, 0, err
	}
	defer input.Close()
	prefix := make([]byte, len(magicHeader)+1)
	n, readErr := io.ReadFull(input, prefix)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return "", false, 0, readErr
	}
	if n < len(magicHeader) || !bytes.Equal(prefix[:len(magicHeader)], magicHeader) {
		if passphrase != "" {
			return "", false, 0, errors.New("passphrase provided but backup is not encrypted")
		}
		return path, false, 0, nil
	}
	if passphrase == "" {
		return "", true, 0, errors.New("passphrase is required to decrypt this backup")
	}
	version := int(prefix[len(magicHeader)])
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return "", true, version, err
	}
	outputPath := filepath.Join(workDir, "decrypted.tar.gz")
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", true, version, err
	}
	limitedOutput := &backupPolicyWriter{writer: output, maxBytes: maxBytes}
	if version == int(chunkedEncryptionVersion) {
		err = decryptChunkedBackup(input, limitedOutput, passphrase)
	} else if version == 1 || version == 2 {
		// Legacy single-message GCM cannot be authenticated incrementally. This
		// bounded compatibility path is retained only for existing archives; all
		// newly created file backups use chunked v3.
		legacyData, readErr := io.ReadAll(io.LimitReader(input, maxBytes+1))
		if readErr != nil {
			err = readErr
		} else if int64(len(legacyData)) > maxBytes {
			err = backupPolicyError(int64(len(legacyData)), maxBytes)
		} else {
			var plaintext []byte
			plaintext, _, _, err = decryptBackup(legacyData, passphrase)
			if err == nil {
				_, err = limitedOutput.Write(plaintext)
			}
		}
	} else {
		err = fmt.Errorf("unsupported backup format version: %d", version)
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", true, version, err
	}
	return outputPath, true, version, nil
}

func consistentSQLiteSnapshotFile(databasePath, workDir, stateDigest string) (string, uint64, error) {
	info, err := os.Lstat(databasePath)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, errors.New("backup database is not a regular file")
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		return "", 0, err
	}
	defer db.Close()
	tmp, err := os.CreateTemp(workDir, "veil-db-*.sqlite")
	if err != nil {
		return "", 0, err
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", 0, err
	}
	_ = os.Remove(path)
	quoted := "'" + strings.ReplaceAll(path, "'", "''") + "'"
	if _, err := db.Exec(`VACUUM INTO ` + quoted); err != nil {
		return "", 0, err
	}
	snapshotInfo, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if snapshotInfo.Size() == 0 {
		return "", 0, errors.New("SQLite snapshot is empty")
	}
	desiredRevision, err := validateSQLiteDesiredSnapshotPath(path, nil, stateDigest)
	if err != nil {
		return "", 0, err
	}
	return path, desiredRevision, nil
}

func validateSQLiteSnapshotFile(path string, expectedDesiredRevision *uint64, expectedStateDigest string) error {
	db, err := storage.OpenExisting(path)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite quick_check: %s", result)
	}
	_, err = validateSQLiteDesiredSnapshotDB(db, expectedDesiredRevision, expectedStateDigest)
	return err
}

func archiveFileMetadata(name, path string) (ArchiveFile, error) {
	file, err := openBackupRegularNoFollow(path)
	if err != nil {
		return ArchiveFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ArchiveFile{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ArchiveFile{}, err
	}
	return ArchiveFile{Name: name, Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func copySnapshotFile(source, destination string, maxBytes int64) (string, error) {
	input, err := openBackupRegularNoFollow(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("backup source is not a regular file")
	}
	if info.Size() < 0 || info.Size() > maxBytes {
		return "", backupPolicyError(info.Size(), maxBytes)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	written, copyErr := io.CopyN(output, input, info.Size())
	if copyErr == nil && written != info.Size() {
		copyErr = io.ErrUnexpectedEOF
	}
	if syncErr := output.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := output.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return "", copyErr
	}
	return destination, nil
}

func rawStateSchemaVersionFile(path string) (int, error) {
	file, err := openBackupRegularNoFollow(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var raw struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.NewDecoder(file).Decode(&raw); err != nil {
		return 0, err
	}
	if raw.SchemaVersion <= 0 {
		return 1, nil
	}
	return raw.SchemaVersion, nil
}

func writeTarFileStream(writer *tar.Writer, file streamArchiveFile) error {
	info, err := os.Stat(file.path)
	if err != nil {
		return err
	}
	header := &tar.Header{Name: file.name, Mode: file.mode, Size: info.Size(), Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	input, err := openBackupRegularNoFollow(file.path)
	if err != nil {
		return err
	}
	defer input.Close()
	written, err := io.CopyN(writer, input, info.Size())
	if err != nil {
		return err
	}
	if written != info.Size() {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type backupPolicyWriter struct {
	writer   io.Writer
	written  int64
	maxBytes int64
}

func (w *backupPolicyWriter) Write(body []byte) (int, error) {
	if int64(len(body)) > w.maxBytes-w.written {
		return 0, backupPolicyError(w.written+int64(len(body)), w.maxBytes)
	}
	n, err := w.writer.Write(body)
	w.written += int64(n)
	return n, err
}

func normalizeBackupMaxBytes(configured int64) (int64, error) {
	if configured == 0 {
		return DefaultMaxBackupBytes, nil
	}
	if configured < 0 {
		return 0, errors.New("configured backup size policy must be positive")
	}
	return configured, nil
}

func addPolicyBytes(current, addition, maxBytes int64) (int64, error) {
	if addition < 0 || current > maxBytes-addition {
		return 0, backupPolicyError(current+addition, maxBytes)
	}
	return current + addition, nil
}

func backupPolicyError(size, maxBytes int64) error {
	return fmt.Errorf("configured backup size policy exceeded: %d bytes > %d bytes", size, maxBytes)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func stageRestoreFileFromPath(target, source, safety string) (*stagedRestoreFile, error) {
	if err := restoreMkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	input, err := openBackupRegularNoFollow(source)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	temp, err := restoreCreateTemp(filepath.Dir(target), ".veil-restore-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	if _, err := io.Copy(temp, input); err != nil {
		_ = restoreFileClose(temp)
		_ = restoreRemove(tempPath)
		return nil, err
	}
	if err := restoreFileSync(temp); err != nil {
		_ = restoreFileClose(temp)
		_ = restoreRemove(tempPath)
		return nil, err
	}
	if err := restoreFileClose(temp); err != nil {
		_ = restoreRemove(tempPath)
		return nil, err
	}
	mode := os.FileMode(0o600)
	var info os.FileInfo
	if existing, statErr := os.Stat(target); statErr == nil {
		info = existing
		mode = existing.Mode().Perm()
	}
	if err := restoreChmod(tempPath, mode); err != nil {
		_ = restoreRemove(tempPath)
		return nil, err
	}
	if info != nil {
		if err := restoreChownToMatch(tempPath, info); err != nil {
			_ = restoreRemove(tempPath)
			return nil, err
		}
	}
	_, statErr := os.Stat(target)
	return &stagedRestoreFile{target: target, temp: tempPath, safety: safety, hadOriginal: statErr == nil}, nil
}

type chunkEncryptWriter struct {
	destination io.Writer
	aead        cipher.AEAD
	header      []byte
	baseNonce   []byte
	buffer      []byte
	frame       uint64
	closed      bool
}

func newChunkEncryptWriter(destination io.Writer, passphrase string) (*chunkEncryptWriter, error) {
	salt := make([]byte, 16)
	if _, err := backupRandRead(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	nonce := make([]byte, 12)
	if _, err := backupRandRead(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	key := deriveKey(passphrase, salt, chunkedEncryptionVersion)
	block, err := encryptAESNewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := encryptNewGCM(block)
	if err != nil {
		return nil, err
	}
	header := append(append(append([]byte{}, magicHeader...), chunkedEncryptionVersion), salt...)
	header = append(header, nonce...)
	if err := writeBackupAll(destination, header); err != nil {
		return nil, err
	}
	return &chunkEncryptWriter{destination: destination, aead: aead, header: header, baseNonce: nonce, buffer: make([]byte, 0, backupChunkBytes)}, nil
}

func (w *chunkEncryptWriter) Write(body []byte) (int, error) {
	if w.closed {
		return 0, errors.New("write to closed backup encryption stream")
	}
	accepted := 0
	for len(body) > 0 {
		space := backupChunkBytes - len(w.buffer)
		if space > len(body) {
			space = len(body)
		}
		w.buffer = append(w.buffer, body[:space]...)
		body = body[space:]
		accepted += space
		if len(w.buffer) == backupChunkBytes {
			if err := w.flushFrame(w.buffer); err != nil {
				return accepted, err
			}
			w.buffer = w.buffer[:0]
		}
	}
	return accepted, nil
}

func (w *chunkEncryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if len(w.buffer) > 0 {
		if err := w.flushFrame(w.buffer); err != nil {
			return err
		}
	}
	// An authenticated empty terminal frame proves that the stream was not
	// truncated after a valid data frame.
	return w.flushFrame(nil)
}

func (w *chunkEncryptWriter) flushFrame(plaintext []byte) error {
	if len(plaintext) > backupChunkBytes {
		return errors.New("backup encryption frame exceeds chunk size")
	}
	length := uint32(len(plaintext))
	lengthBody := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBody, length)
	nonce := chunkNonce(w.baseNonce, w.frame)
	aad := chunkAAD(w.header, w.frame, length)
	ciphertext := w.aead.Seal(nil, nonce, plaintext, aad)
	if err := writeBackupAll(w.destination, lengthBody); err != nil {
		return err
	}
	if err := writeBackupAll(w.destination, ciphertext); err != nil {
		return err
	}
	w.frame++
	return nil
}

func decryptChunkedBackup(source io.Reader, destination io.Writer, passphrase string) error {
	header := make([]byte, len(magicHeader)+1+16+12)
	if _, err := io.ReadFull(source, header); err != nil {
		return errors.New("invalid or corrupted encrypted backup file (too short)")
	}
	if !bytes.Equal(header[:len(magicHeader)], magicHeader) || header[len(magicHeader)] != chunkedEncryptionVersion {
		return errors.New("invalid chunked backup header")
	}
	saltStart := len(magicHeader) + 1
	salt := header[saltStart : saltStart+16]
	baseNonce := header[saltStart+16:]
	key := deriveKey(passphrase, salt, chunkedEncryptionVersion)
	block, err := decryptAESNewCipher(key)
	if err != nil {
		return err
	}
	aead, err := decryptNewGCM(block)
	if err != nil {
		return err
	}
	for frame := uint64(0); ; frame++ {
		var lengthBody [4]byte
		if _, err := io.ReadFull(source, lengthBody[:]); err != nil {
			return errors.New("failed to decrypt backup: truncated authenticated stream")
		}
		length := binary.BigEndian.Uint32(lengthBody[:])
		if length > backupChunkBytes {
			return errors.New("failed to decrypt backup: invalid encrypted frame size")
		}
		ciphertext := make([]byte, int(length)+aead.Overhead())
		if _, err := io.ReadFull(source, ciphertext); err != nil {
			return errors.New("failed to decrypt backup: truncated authenticated frame")
		}
		plaintext, err := aead.Open(nil, chunkNonce(baseNonce, frame), ciphertext, chunkAAD(header, frame, length))
		if err != nil {
			return errors.New("failed to decrypt backup: incorrect passphrase or corrupted data")
		}
		if length == 0 {
			var trailing [1]byte
			n, err := source.Read(trailing[:])
			if n != 0 || (err != nil && err != io.EOF) || err == nil {
				return errors.New("failed to decrypt backup: trailing data after terminal frame")
			}
			return nil
		}
		if err := writeBackupAll(destination, plaintext); err != nil {
			return err
		}
	}
}

func writeBackupAll(destination io.Writer, body []byte) error {
	for len(body) > 0 {
		written, err := destination.Write(body)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(body) {
			return io.ErrShortWrite
		}
		body = body[written:]
	}
	return nil
}

func chunkNonce(base []byte, frame uint64) []byte {
	nonce := append([]byte(nil), base...)
	baseCounter := binary.BigEndian.Uint64(nonce[len(nonce)-8:])
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], baseCounter+frame)
	return nonce
}

func chunkAAD(header []byte, frame uint64, length uint32) []byte {
	aad := make([]byte, len(header)+8+4)
	copy(aad, header)
	binary.BigEndian.PutUint64(aad[len(header):], frame)
	binary.BigEndian.PutUint32(aad[len(header)+8:], length)
	return aad
}
