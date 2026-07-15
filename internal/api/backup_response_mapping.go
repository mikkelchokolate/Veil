package api

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func backupCreateResponseFromPrivileged(backupDir string, result privileged.BackupResult) BackupCreateResponse {
	archive := backup.ArchiveEntry{
		Name:      result.ArchiveName,
		Path:      filepath.Join(backupDir, result.ArchiveName),
		CreatedAt: time.Now().UTC(),
		Encrypted: result.Verified,
	}
	for _, candidate := range result.Archives {
		if candidate.Name != result.ArchiveName {
			continue
		}
		archive.Size = candidate.Size
		archive.Encrypted = candidate.Encrypted
		if createdAt, err := time.Parse(time.RFC3339, candidate.CreatedAt); err == nil {
			archive.CreatedAt = createdAt
		}
		break
	}
	return BackupCreateResponse{
		Archive:      archive,
		Verification: backupVerificationFromPrivileged(result),
		Warning:      result.Warning,
	}
}

func backupVerificationFromPrivileged(result privileged.BackupResult) backup.VerificationReport {
	if result.Verification == nil {
		return backup.VerificationReport{Encrypted: result.Verified, Files: []backup.ArchiveFile{}}
	}
	files := make([]backup.ArchiveFile, 0, len(result.Verification.Files))
	for _, file := range result.Verification.Files {
		files = append(files, backup.ArchiveFile{Name: file.Name, Size: file.Size, SHA256: file.SHA256})
	}
	createdAt := time.Time{}
	if result.Verification.CreatedAt != "" {
		createdAt, _ = time.Parse(time.RFC3339, result.Verification.CreatedAt)
	}
	return backup.VerificationReport{
		FormatVersion:      result.Verification.FormatVersion,
		EncryptionVersion:  result.Verification.EncryptionVersion,
		Encrypted:          result.Verification.Encrypted,
		Legacy:             result.Verification.Legacy,
		CreatedAt:          createdAt,
		VeilVersion:        result.Verification.VeilVersion,
		StateSchemaVersion: result.Verification.StateSchemaVersion,
		Files:              files,
	}
}

func appendBackupResponseWarning(existing, warning string) string {
	warning = strings.TrimSpace(warning)
	if warning == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return warning
	}
	return existing + "; " + warning
}
