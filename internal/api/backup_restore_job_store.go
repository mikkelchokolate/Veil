package api

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
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
	now := time.Now().UTC()
	changed := false
	for _, job := range stored.Jobs {
		if job.ID == "" || job.Archive == "" {
			return fmt.Errorf("invalid restore job history entry")
		}
		if job.Status == "queued" || job.Status == "running" {
			job.Status = "failed"
			job.Error = "restore interrupted by panel restart"
			job.FinishedAt = now
			changed = true
		}
		s.backupJobs[job.ID] = job
	}
	if changed {
		s.backupJobsMu.Lock()
		err = s.persistBackupRestoreJobsLocked()
		s.backupJobsMu.Unlock()
		return err
	}
	return nil
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
