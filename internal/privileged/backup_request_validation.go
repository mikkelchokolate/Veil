package privileged

func validateBackupRequest(request BackupRequest) error {
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
