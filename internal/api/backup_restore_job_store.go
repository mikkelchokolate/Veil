package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/backup"
)

const (
	maxBackupRestoreJobStoreBytes = 1024 * 1024
	maxPersistedBackupRestoreJobs = 500
)

type backupRestoreJobStoreFile struct {
	Version int                `json:"version"`
	Jobs    []BackupRestoreJob `json:"jobs"`
}

func (s *managementState) loadBackupRestoreJobs() error {
	if s.backupJobsPath == "" {
		return nil
	}
	file, err := os.Open(s.backupJobsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open restore job history: %w", err)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxBackupRestoreJobStoreBytes+1))
	if err != nil {
		return fmt.Errorf("read restore job history: %w", err)
	}
	if len(body) > maxBackupRestoreJobStoreBytes {
		return fmt.Errorf("restore job history exceeds size limit")
	}
	var stored backupRestoreJobStoreFile
	if err := json.Unmarshal(body, &stored); err != nil {
		return fmt.Errorf("decode restore job history: %w", err)
	}
	if stored.Version != 1 || len(stored.Jobs) > maxPersistedBackupRestoreJobs {
		return fmt.Errorf("invalid restore job history")
	}
	// Validate every entry before adopting any, so a rejected history is
	// rejected as a whole instead of being half-loaded.
	for _, job := range stored.Jobs {
		if err := validatePersistedBackupRestoreJob(job); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	changed := false
	for _, job := range stored.Jobs {
		if job.Status == "queued" || job.Status == "running" {
			committed := false
			if job.Status == "running" {
				ok, commitErr := backup.RestoreTransactionCommitted(s.statePath, s.keyPath, s.restoreDatabasePath())
				committed = commitErr == nil && ok
			}
			if committed {
				job.Status = "degraded"
				job.Outcome = "restored"
				job.Phase = "revalidation_failed"
				job.Restored = true
				job.HTTPStatus = http.StatusInternalServerError
				job.Error = "restore committed before panel restart; reopen and convergence did not finish"
			} else {
				job.Status = "failed"
				job.Outcome = "not_restored"
				job.Phase = "restore_interrupted"
				job.Restored = false
				job.HTTPStatus = http.StatusInternalServerError
				job.Error = "restore interrupted by panel restart"
			}
			job.FinishedAt = now
			changed = true
		}
		s.backupJobs[job.ID] = job
	}
	if changed {
		var clearErr error
		if root := s.restoreStateRoot(); root != "" {
			clearErr = backup.ClearRestoreCommitReceipt(root)
		}
		s.backupJobsMu.Lock()
		persistErr := s.persistBackupRestoreJobsLocked()
		s.backupJobsMu.Unlock()
		return errors.Join(persistErr, clearErr)
	}
	return nil
}

// backupRestoreJobStatuses is the closed set of job statuses the panel
// writes (see BackupRestoreJob.status in docs/openapi.yaml).
var backupRestoreJobStatuses = map[string]bool{
	"queued": true, "running": true, "pending": true,
	"succeeded": true, "failed": true, "degraded": true,
}

// validBackupRestoreJobHTTPStatus reports whether a recorded job HTTP status
// can be replayed by handleBackupRestoreJob. 0 is the legacy "unset" value
// (served as 200); anything else must be a real final status code, because
// http.ResponseWriter.WriteHeader panics outside [100, 999] and 1xx is not a
// final response (#1268 item 4).
func validBackupRestoreJobHTTPStatus(code int) bool {
	return code == 0 || (code >= 200 && code <= 599)
}

// validatePersistedBackupRestoreJob rejects history entries the panel could
// not have written. backup-restore-jobs.json lives in the veil-writable state
// directory, so its contents are not trusted beyond what this checks.
func validatePersistedBackupRestoreJob(job BackupRestoreJob) error {
	if job.ID == "" || job.Archive == "" {
		return fmt.Errorf("invalid restore job history entry")
	}
	if !backupRestoreJobStatuses[job.Status] {
		return fmt.Errorf("invalid restore job history entry %q: unknown status %q", job.ID, job.Status)
	}
	if !validBackupRestoreJobHTTPStatus(job.HTTPStatus) {
		return fmt.Errorf("invalid restore job history entry %q: http status %d out of range", job.ID, job.HTTPStatus)
	}
	return nil
}

func (s *managementState) restoreStateRoot() string {
	if s.statePath == "" {
		return ""
	}
	return filepath.Dir(s.statePath)
}

func (s *managementState) restoreDatabasePath() string {
	if s.statePath == "" {
		return ""
	}
	path := filepath.Join(filepath.Dir(s.statePath), "veil.db")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

func (s *managementState) persistBackupRestoreJobsLocked() error {
	if s.backupJobsPath == "" {
		return nil
	}
	jobs := make([]BackupRestoreJob, 0, len(s.backupJobs))
	for _, job := range s.backupJobs {
		job.ownerSessionToken = ""
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].ID < jobs[j].ID
		}
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	if len(jobs) > maxPersistedBackupRestoreJobs {
		jobs = jobs[len(jobs)-maxPersistedBackupRestoreJobs:]
	}
	body, err := json.Marshal(backupRestoreJobStoreFile{Version: 1, Jobs: jobs})
	if err != nil {
		return fmt.Errorf("encode restore job history: %w", err)
	}
	if err := atomicfile.Write(s.backupJobsPath, body, 0o600, 0o700); err != nil {
		return fmt.Errorf("persist restore job history: %w", err)
	}
	return nil
}
