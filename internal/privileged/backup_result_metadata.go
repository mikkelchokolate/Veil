package privileged

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

type BackupVerificationFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type BackupVerificationReport struct {
	FormatVersion      int                      `json:"formatVersion"`
	EncryptionVersion  int                      `json:"encryptionVersion"`
	Encrypted          bool                     `json:"encrypted"`
	Legacy             bool                     `json:"legacy"`
	CreatedAt          string                   `json:"createdAt,omitempty"`
	VeilVersion        string                   `json:"veilVersion,omitempty"`
	StateSchemaVersion int                      `json:"stateSchemaVersion"`
	Files              []BackupVerificationFile `json:"files"`
}

func enrichBackupResult(request ResolvedBackup, result BackupResult) BackupResult {
	if result.Verification != nil || !result.Verified {
		return result
	}
	if request.Action != BackupActionCreate && request.Action != BackupActionVerify {
		return result
	}
	archivePath := request.ArchivePath
	if request.Action == BackupActionCreate && result.ArchiveName != "" {
		archivePath = filepath.Join(request.BackupRoot, result.ArchiveName)
	}
	if archivePath == "" {
		result.Warning = appendBackupResultWarning(result.Warning, "backup metadata path is unavailable")
		return result
	}
	data, err := readBoundedRegularFile(archivePath, maxBackupReadBytes)
	if err != nil {
		result.Warning = appendBackupResultWarning(result.Warning, "read backup metadata: "+err.Error())
		return result
	}
	passphraseBody, err := os.ReadFile(request.BackupPassphrasePath)
	if err != nil {
		result.Warning = appendBackupResultWarning(result.Warning, "read backup metadata passphrase: "+err.Error())
		return result
	}
	report, err := backup.VerifyBackup(data, strings.TrimRight(string(passphraseBody), "\r\n"))
	if err != nil {
		result.Warning = appendBackupResultWarning(result.Warning, "verify backup metadata: "+err.Error())
		return result
	}
	result.Verification = backupVerificationReportFromArchive(report)
	if request.Action == BackupActionCreate {
		result.Archives = []BackupArchive{{
			Name:      result.ArchiveName,
			Size:      int64(len(data)),
			CreatedAt: report.CreatedAt.UTC().Format(timeRFC3339),
			Encrypted: report.Encrypted,
		}}
	}
	return result
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

func backupVerificationReportFromArchive(report backup.VerificationReport) *BackupVerificationReport {
	files := make([]BackupVerificationFile, 0, len(report.Files))
	for _, file := range report.Files {
		files = append(files, BackupVerificationFile{Name: file.Name, Size: file.Size, SHA256: file.SHA256})
	}
	createdAt := ""
	if !report.CreatedAt.IsZero() {
		createdAt = report.CreatedAt.UTC().Format(timeRFC3339)
	}
	return &BackupVerificationReport{
		FormatVersion:      report.FormatVersion,
		EncryptionVersion:  report.EncryptionVersion,
		Encrypted:          report.Encrypted,
		Legacy:             report.Legacy,
		CreatedAt:          createdAt,
		VeilVersion:        report.VeilVersion,
		StateSchemaVersion: report.StateSchemaVersion,
		Files:              files,
	}
}

func appendBackupResultWarning(existing, warning string) string {
	warning = strings.TrimSpace(warning)
	if warning == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return warning
	}
	return existing + "; " + warning
}
