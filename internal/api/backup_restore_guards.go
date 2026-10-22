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

// beginBackupRead fences a streaming read (archive download) against
// prune/delete/restore mutations while still allowing concurrent downloads.
func (s *managementState) beginBackupRead(w http.ResponseWriter) bool {
	if s.backupMutationMu.TryRLock() {
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
		// Every terminal status is prunable: degraded marks a restore that
		// committed but failed revalidation/finalization, and pending marks a
		// restore whose key publication is outstanding — neither advances
		// further once recorded, so they must not accumulate forever.
		switch job.Status {
		case "succeeded", "failed", "degraded", "pending":
		default:
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

// scheduleBackupRestoreOwnerSessionRevocation ends the restore owner's
// pre-restore cookie session after a succeeded restore. An owner who no
// longer validates against the restored users/roles is revoked at once, and
// so is one whose revalidation errors (fail closed: an owner that cannot be
// verified does not get a grace). Otherwise the session survives only for
// the owner-session grace so the SPA can read the terminal job, then a timer
// revokes it. The revocation is
// deliberately NOT triggered by reading the job: GET
// /api/backup-restore-jobs/{id} is a safe method, and SameSite=Lax sends the
// cookie on cross-site top-level navigation, so a read-triggered revocation
// let a crafted link force-log-out the owner and left the session alive to
// natural expiry when the job was never polled (#1266, #1268).
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
	if err != nil || !valid {
		s.revokeBackupRestoreOwnerSession(jobID, token)
		return
	}
	grace := s.backupRestoreOwnerSessionGraceDuration()
	if grace <= 0 {
		s.revokeBackupRestoreOwnerSession(jobID, token)
		return
	}
	s.backupJobsMu.Lock()
	if s.backupOwnerRevocations == nil {
		s.backupOwnerRevocations = make(map[string]backupOwnerRevocation)
	}
	replacedToken := ""
	if previous, ok := s.backupOwnerRevocations[jobID]; ok && previous.timer.Stop() && previous.token != token {
		// A replaced, never-fired grace must still end its own session.
		replacedToken = previous.token
	}
	var timer *time.Timer
	timer = time.AfterFunc(grace, func() {
		s.backupJobsMu.Lock()
		// Only drop the entry if it is still this timer's: a re-armed job
		// id must keep its replacement visible to the Close() flush.
		if current, ok := s.backupOwnerRevocations[jobID]; ok && current.timer == timer {
			delete(s.backupOwnerRevocations, jobID)
		}
		s.backupJobsMu.Unlock()
		s.revokeBackupRestoreOwnerSession(jobID, token)
	})
	s.backupOwnerRevocations[jobID] = backupOwnerRevocation{timer: timer, token: token}
	s.backupJobsMu.Unlock()
	if replacedToken != "" {
		s.revokeBackupRestoreOwnerSession(jobID, replacedToken)
	}
}

// backupOwnerRevocation is one pending owner-session grace. The token is
// kept with the timer so a flush does not depend on the job still being
// retained (pruning must not let the session outlive the grace).
type backupOwnerRevocation struct {
	timer *time.Timer
	token string
}

// backupRestoreOwnerSessionGraceDuration returns the configured grace, the
// default when unset, and a non-positive value (revoke immediately) when a
// caller set it negative.
func (s *managementState) backupRestoreOwnerSessionGraceDuration() time.Duration {
	if s.backupRestoreOwnerSessionGrace == 0 {
		return defaultBackupRestoreOwnerSessionGrace
	}
	return s.backupRestoreOwnerSessionGrace
}

// flushBackupRestoreOwnerSessionRevocations stops every pending grace timer
// and revokes its owner session now. The grace is an in-process courtesy that
// cannot survive a restart (owner tokens are never persisted with the job),
// so shutdown must not leave the session alive to natural expiry.
func (s *managementState) flushBackupRestoreOwnerSessionRevocations() {
	type pending struct{ jobID, token string }
	s.backupJobsMu.Lock()
	var due []pending
	for jobID, entry := range s.backupOwnerRevocations {
		if entry.timer.Stop() {
			due = append(due, pending{jobID, entry.token})
		}
		delete(s.backupOwnerRevocations, jobID)
	}
	s.backupJobsMu.Unlock()
	for _, p := range due {
		s.revokeBackupRestoreOwnerSession(p.jobID, p.token)
	}
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
