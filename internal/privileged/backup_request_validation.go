package privileged

import "github.com/mikkelchokolate/Veil/internal/backup"

const maxBackupReadChunkBytes int64 = 1024 * 1024

func validateBackupRequest(request BackupRequest) error {
	if request.Offset < 0 {
		return newError(ErrorInvalidRequest, "backup read offset must not be negative")
	}
	if request.Limit < 0 || request.Limit > maxBackupReadChunkBytes {
		return newError(ErrorInvalidRequest, "backup read limit must be between 0 and 1048576 bytes")
	}
	if request.Action != BackupActionRead && (request.Offset != 0 || request.Limit != 0) {
		return newError(ErrorInvalidRequest, "backup read offset and limit are only valid for read")
	}
	if err := backup.ValidateRetentionCounts(request.Daily, request.Weekly, request.Monthly); err != nil {
		return newError(ErrorInvalidRequest, err.Error())
	}
	return nil
}
