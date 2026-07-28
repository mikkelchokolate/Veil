package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupRestoreJobsPersistAndRunningJobsBecomeFailedOnRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup-restore-jobs.json")
	created := time.Now().UTC().Add(-time.Minute)
	first := &managementState{
		backupJobsPath: path,
		backupJobs: map[string]BackupRestoreJob{
			"job-1": {ID: "job-1", Archive: "veil_backup_20260728_120000.tar.gz.enc", Status: "running", CreatedAt: created, StartedAt: created},
		},
	}
	first.backupJobsMu.Lock()
	err := first.persistBackupRestoreJobsLocked()
	first.backupJobsMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}

	second := &managementState{backupJobsPath: path, backupJobs: make(map[string]BackupRestoreJob)}
	if err := second.loadBackupRestoreJobs(); err != nil {
		t.Fatal(err)
	}
	job, ok := second.backupJobs["job-1"]
	if !ok || job.Status != "failed" || job.FinishedAt.IsZero() || job.Error != "restore interrupted by panel restart" {
		t.Fatalf("job=%+v ok=%v", job, ok)
	}
}
