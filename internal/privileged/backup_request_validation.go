package privileged

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
	if request.Daily < 0 || request.Daily > 365 {
		return newError(ErrorInvalidRequest, "daily retention must be between 0 and 365")
	}
	if request.Weekly < 0 || request.Weekly > 104 {
		return newError(ErrorInvalidRequest, "weekly retention must be between 0 and 104")
	}
	if request.Monthly < 0 || request.Monthly > 120 {
		return newError(ErrorInvalidRequest, "monthly retention must be between 0 and 120")
	}
	return nil
}
