package installer

type BackupLifecycle struct {
	Dir string
}

func NewBackupLifecycle(dir string) BackupLifecycle {
	return BackupLifecycle{Dir: dir}
}

func (l BackupLifecycle) BackupExisting(paths []string) (string, error) {
	return BackupBeforeApply(paths, l.Dir)
}

func (l BackupLifecycle) Restore(backupID string) ([]string, error) {
	return RestoreFromBackup(l.Dir, backupID)
}

func (l BackupLifecycle) Cleanup(backupID string) error {
	return CleanupBackup(l.Dir, backupID)
}

func (l BackupLifecycle) List() ([]string, error) {
	return ListBackups(l.Dir)
}
