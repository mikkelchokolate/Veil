package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"
)

const (
	defaultBackupRestoreOwnerSessionGrace = 30 * time.Second
	maxRetainedBackupRestoreJobs          = 100
)

func (s *managementState) beginBackupMutation(w http.ResponseWriter) bool {
	if s.backupMutationMu.TryLock() {
		s.pruneBackupRestoreJobs()
		return true
	}
	writeError(w, "another backup operation is already in progress", http.StatusConflict)
	return false
}

func (s *managementState) pruneBackupRestoreJobs() {
	s.backupJobsMu.Lock()
	defer s.backupJobsMu.Unlock()
	if len(s.backupJobs) < maxRetainedBackupRestoreJobs {
		return
	}
	type candidate struct {
		id   string
		when time.Time
	}
	candidates := make([]candidate, 0, len(s.backupJobs))
	for id, job := range s.backupJobs {
		if job.Status != "succeeded" && job.Status != "failed" {
			continue
		}
		when := job.FinishedAt
		if when.IsZero() {
			when = job.CreatedAt
		}
		candidates = append(candidates, candidate{id: id, when: when})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].when.Equal(candidates[j].when) {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].when.Before(candidates[j].when)
	})
	for len(s.backupJobs) >= maxRetainedBackupRestoreJobs && len(candidates) > 0 {
		delete(s.backupJobs, candidates[0].id)
		candidates = candidates[1:]
	}
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
	s.mu.Lock()
	roles := make(map[string]string, len(s.users))
	for _, user := range s.users {
		roles[user.Username] = user.Role
	}
	s.mu.Unlock()
	valid, err := s.sessionRegistry().RevalidateToken(token, roles)
	if err != nil || valid {
		return
	}
	s.revokeBackupRestoreOwnerSession(jobID, token)
}

func (s *managementState) revokeBackupRestoreOwnerSession(jobID, token string) {
	if token == "" || s.sessions == nil {
		return
	}
	if _, err := s.sessionRegistry().DeleteTokenPersisted(token); err != nil {
		return
	}
	s.backupJobsMu.Lock()
	defer s.backupJobsMu.Unlock()
	job, ok := s.backupJobs[jobID]
	if !ok || job.ownerSessionToken != token {
		return
	}
	job.ownerSessionToken = ""
	s.backupJobs[jobID] = job
}
