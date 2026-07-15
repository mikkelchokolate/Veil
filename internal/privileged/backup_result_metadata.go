package privileged

import (
	"os"
	"path/filepath"
	"strings"
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
	if request.Action != BackupActionCreate || result.ArchiveName == "" || len(result.Archives) > 0 {
		return result
	}
	archivePath := filepath.Join(request.BackupRoot, result.ArchiveName)
	info, err := os.Stat(archivePath)
	if err != nil {
		result.Warning = appendBackupResultWarning(result.Warning, "read backup metadata: "+err.Error())
		return result
	}
	if !info.Mode().IsRegular() {
		result.Warning = appendBackupResultWarning(result.Warning, "backup metadata target is not a regular file")
		return result
	}
	result.Archives = []BackupArchive{{
		Name:      result.ArchiveName,
		Size:      info.Size(),
		CreatedAt: info.ModTime().UTC().Format(timeRFC3339),
		Encrypted: result.Verified,
	}}
	return result
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

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
