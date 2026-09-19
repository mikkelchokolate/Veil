package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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

func TestBackupRestoreJobsKeepHelperCommittedRestoreAcrossRestart(t *testing.T) {
	root := t.TempDir()
	jobsPath := filepath.Join(root, "backup-restore-jobs.json")
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	intendedState, intendedKey := []byte("restored-state"), []byte("restored-key")
	if err := os.WriteFile(statePath, intendedState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, intendedKey, 0o600); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Minute)
	running := BackupRestoreJob{ID: "job-committed", Archive: "veil_backup_20260728_120000.tar.gz.enc", Status: "running", CreatedAt: created, StartedAt: created}
	persistRunningRestoreJob(t, jobsPath, running)

	writeAPIRestoreJournal(t, root, "prepared", restoreJobChecksum(intendedState), restoreJobChecksum(intendedKey))
	reloaded := loadRestoreJobsFromDisk(t, jobsPath, statePath, keyPath)
	assertHelperCommittedRestoreJob(t, reloaded.backupJobs["job-committed"])

	persistRunningRestoreJob(t, jobsPath, running)
	if err := os.Remove(filepath.Join(root, ".veil-restore-journal.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".veil-restore-committed"), []byte(`{"version":1,"committed":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded = loadRestoreJobsFromDisk(t, jobsPath, statePath, keyPath)
	assertHelperCommittedRestoreJob(t, reloaded.backupJobs["job-committed"])
	if _, err := os.Stat(filepath.Join(root, ".veil-restore-committed")); !os.IsNotExist(err) {
		t.Fatalf("commit receipt should be consumed after job finalization, err=%v", err)
	}
}

func TestBackupRestoreJobsFailUncommittedHelperJournalAcrossRestart(t *testing.T) {
	root := t.TempDir()
	jobsPath := filepath.Join(root, "backup-restore-jobs.json")
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	if err := os.WriteFile(statePath, []byte("live-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("live-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Minute)
	persistRunningRestoreJob(t, jobsPath, BackupRestoreJob{ID: "job-open", Archive: "veil_backup_20260728_120000.tar.gz.enc", Status: "running", CreatedAt: created, StartedAt: created})
	writeAPIRestoreJournal(t, root, "prepared", restoreJobChecksum([]byte("intended-state")), restoreJobChecksum([]byte("intended-key")))

	reloaded := loadRestoreJobsFromDisk(t, jobsPath, statePath, keyPath)
	job := reloaded.backupJobs["job-open"]
	if job.Status != "failed" || job.Restored || job.Error != "restore interrupted by panel restart" {
		t.Fatalf("uncommitted journal job=%+v", job)
	}
}

func TestUpdateBackupRestoreJobFailsWhenCommitReceiptCannotClear(t *testing.T) {
	root := t.TempDir()
	// A non-empty directory at the receipt path makes os.Remove fail, which
	// must propagate instead of advancing the job bookkeeping on a stale
	// commit receipt.
	if err := os.MkdirAll(filepath.Join(root, ".veil-restore-committed", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	state := &managementState{
		statePath: filepath.Join(root, "state.json"),
		backupJobs: map[string]BackupRestoreJob{
			"job-1": {ID: "job-1", Status: "running"},
		},
	}
	err := state.updateBackupRestoreJob("job-1", func(job *BackupRestoreJob) {
		job.Status = "succeeded"
	})
	if err == nil {
		t.Fatal("commit receipt clear failure was discarded")
	}
	if got := state.backupJobs["job-1"].Status; got != "running" {
		t.Fatalf("job bookkeeping advanced to %q despite receipt clear failure", got)
	}
}

func TestLoadBackupRestoreJobsSurfacesCommitReceiptClearFailure(t *testing.T) {
	root := t.TempDir()
	jobsPath := filepath.Join(root, "backup-restore-jobs.json")
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	if err := os.WriteFile(statePath, []byte("live-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("live-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Minute)
	persistRunningRestoreJob(t, jobsPath, BackupRestoreJob{ID: "job-open", Archive: "veil_backup_20260728_120000.tar.gz.enc", Status: "running", CreatedAt: created, StartedAt: created})
	if err := os.MkdirAll(filepath.Join(root, ".veil-restore-committed", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}

	state := &managementState{backupJobsPath: jobsPath, backupJobs: make(map[string]BackupRestoreJob), statePath: statePath, keyPath: keyPath}
	if err := state.loadBackupRestoreJobs(); err == nil {
		t.Fatal("commit receipt clear failure was discarded during job history load")
	}
}

func persistRunningRestoreJob(t *testing.T, path string, job BackupRestoreJob) {
	t.Helper()
	state := &managementState{backupJobsPath: path, backupJobs: map[string]BackupRestoreJob{job.ID: job}}
	state.backupJobsMu.Lock()
	err := state.persistBackupRestoreJobsLocked()
	state.backupJobsMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}

func loadRestoreJobsFromDisk(t *testing.T, jobsPath, statePath, keyPath string) *managementState {
	t.Helper()
	state := &managementState{backupJobsPath: jobsPath, backupJobs: make(map[string]BackupRestoreJob), statePath: statePath, keyPath: keyPath}
	if err := state.loadBackupRestoreJobs(); err != nil {
		t.Fatal(err)
	}
	return state
}

func assertHelperCommittedRestoreJob(t *testing.T, job BackupRestoreJob) {
	t.Helper()
	if job.Status != "degraded" || job.Outcome != "restored" || job.Phase != "revalidation_failed" || !job.Restored || job.HTTPStatus != http.StatusInternalServerError {
		t.Fatalf("committed restore job=%+v", job)
	}
	if job.Error == "restore interrupted by panel restart" {
		t.Fatal("helper-committed restore was rewritten as interrupted")
	}
}

func writeAPIRestoreJournal(t *testing.T, root, phase, stateDigest, keyDigest string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"version": 2, "transactionId": "tx-panel", "phase": phase,
		"files": []map[string]any{
			{"name": "state.json", "targetId": "state.json", "stagedName": ".restore-state-new", "safetyName": "state.json.pre-restore-test", "hadPrevious": true, "intendedDigest": stateDigest, "mode": 384, "phase": phase},
			{"name": "state.key", "targetId": "state.key", "stagedName": ".restore-key-new", "safetyName": "state.key.pre-restore-test", "hadPrevious": true, "intendedDigest": keyDigest, "mode": 384, "phase": phase},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".veil-restore-journal.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func restoreJobChecksum(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
