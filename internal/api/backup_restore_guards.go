package api

import (
	"fmt"
	"net/http"
	"time"
)

const defaultBackupRestoreOwnerSessionGrace = 30 * time.Second

func (s *managementState) beginBackupMutation(w http.ResponseWriter) bool {
	if s.backupMutationMu.TryLock() {
		return true
	}
	writeError(w, "another backup operation is already in progress", http.StatusConflict)
	return false
}

func validateBackupRetention(daily, weekly, monthly int) error {
	if daily < 0 || daily > 365 {
		return fmt.Errorf("daily retention must be between 0 and 365")
	}
	if weekly < 0 || weekly > 104 {
		return fmt.Errorf("weekly retention must be between 0 and 104")
	}
	if monthly < 0 || monthly > 120 {
		return fmt.Errorf("monthly retention must be between 0 and 120")
	}
	return nil
}

func (s *managementState) scheduleBackupRestoreOwnerSessionRevocation(jobID, token string) {
	if token == "" || s.sessions == nil {
		return
	}
	grace := s.backupRestoreOwnerSessionGrace
	if grace == 0 {
		grace = defaultBackupRestoreOwnerSessionGrace
	}
	if grace < 0 {
		return
	}
	time.AfterFunc(grace, func() {
		s.revokeBackupRestoreOwnerSession(jobID, token)
	})
}

func (s *managementState) revokeBackupRestoreOwnerSession(jobID, token string) {
	if token == "" || s.sessions == nil {
		return
	}
	s.sessionRegistry().Delete(token)
	s.backupJobsMu.Lock()
	defer s.backupJobsMu.Unlock()
	job, ok := s.backupJobs[jobID]
	if !ok || job.ownerSessionToken != token {
		return
	}
	job.ownerSessionToken = ""
	s.backupJobs[jobID] = job
}
