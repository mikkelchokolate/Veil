package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type BackupCreateResponse struct {
	Archive      backup.ArchiveEntry       `json:"archive"`
	Verification backup.VerificationReport `json:"verification"`
	Prune        *backup.PruneResult       `json:"prune,omitempty"`
	Warning      string                    `json:"warning,omitempty"`
}

type BackupRestoreJob struct {
	ID                 string    `json:"id"`
	Archive            string    `json:"archive"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"createdAt"`
	StartedAt          time.Time `json:"startedAt,omitempty"`
	FinishedAt         time.Time `json:"finishedAt,omitempty"`
	Error              string    `json:"error,omitempty"`
	SafetyStatePath    string    `json:"safetyStatePath,omitempty"`
	SafetyKeyPath      string    `json:"safetyKeyPath,omitempty"`
	SafetyDatabasePath string    `json:"safetyDatabasePath,omitempty"`
	ownerSessionToken  string
}

type backupCreateRequest struct {
	Prune   bool `json:"prune"`
	Daily   int  `json:"daily,omitempty"`
	Weekly  int  `json:"weekly,omitempty"`
	Monthly int  `json:"monthly,omitempty"`
}

func (s *managementState) handleBackups(w http.ResponseWriter, r *http.Request) {
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := s.backupOperation(r.Context(), privileged.BackupRequest{Action: privileged.BackupActionList})
		if err != nil {
			writePrivilegedError(w, err)
			return
		}
		writeJSON(w, backupEntriesFromPrivileged(s.backupDir, result.Archives))
	case http.MethodPost:
		var request backupCreateRequest
		if !decodeJSONRequest(w, r, &request) {
			return
		}
		if request.Prune {
			if err := validateBackupRetention(request.Daily, request.Weekly, request.Monthly); err != nil {
				writeError(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if !s.beginBackupMutation(w) {
			return
		}
		defer s.backupMutationMu.Unlock()
		// The helper acquires the cross-process Management snapshot barrier only
		// while it copies state/key and runs SQLite VACUUM INTO; compression and
		// verification do not block unrelated configuration mutations.
		result, err := s.backupOperation(r.Context(), privileged.BackupRequest{Action: privileged.BackupActionCreate})
		if err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.create", Target: "state", Success: false, Error: err.Error()})
			writePrivilegedError(w, err)
			return
		}
		name := result.ArchiveName
		response := backupCreateResponseFromPrivileged(s.backupDir, result)
		details := map[string]any{"prune": request.Prune}
		if request.Prune {
			pruned, pruneErr := s.backupOperation(r.Context(), privileged.BackupRequest{
				Action: privileged.BackupActionPrune, Daily: request.Daily, Weekly: request.Weekly, Monthly: request.Monthly,
			})
			if pruneErr != nil {
				response.Warning = appendBackupResponseWarning(response.Warning, "backup created, but retention prune failed: "+pruneErr.Error())
				details["pruneError"] = pruneErr.Error()
			} else {
				response.Prune = &backup.PruneResult{Deleted: pruned.Pruned, Kept: pruned.Kept}
			}
		}
		s.recordRequestAudit(r, audit.Record{
			Action:  "backup.create",
			Target:  name,
			Success: true,
			Details: details,
		})
		writeJSONStatus(w, http.StatusCreated, response)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *managementState) handleBackupPrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	var request struct {
		Daily   int `json:"daily"`
		Weekly  int `json:"weekly"`
		Monthly int `json:"monthly"`
	}
	if !decodeJSONRequest(w, r, &request) {
		return
	}
	if err := validateBackupRetention(request.Daily, request.Weekly, request.Monthly); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.beginBackupMutation(w) {
		return
	}
	defer s.backupMutationMu.Unlock()
	result, err := s.backupOperation(r.Context(), privileged.BackupRequest{
		Action: privileged.BackupActionPrune, Daily: request.Daily, Weekly: request.Weekly, Monthly: request.Monthly,
	})
	if err != nil {
		writePrivilegedError(w, err)
		return
	}
	s.recordRequestAudit(r, audit.Record{
		Action:  "backup.prune",
		Target:  "managed-backups",
		Success: true,
		Details: map[string]any{"deleted": len(result.Pruned), "kept": len(result.Kept)},
	})
	writeJSON(w, backup.PruneResult{Deleted: result.Pruned, Kept: result.Kept})
}

func (s *managementState) handleBackupByName(w http.ResponseWriter, r *http.Request) {
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	name, action, ok := parsePanelBackupPath(r)
	if !ok {
		writeError(w, "invalid backup archive path", http.StatusBadRequest)
		return
	}
	switch action {
	case "download":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name))
		w.Header().Set("Cache-Control", "no-store")
		var offset int64
		for {
			result, err := s.backupOperation(r.Context(), privileged.BackupRequest{
				Action: privileged.BackupActionRead, ArchiveName: name,
				Offset: offset, Limit: 1024 * 1024,
			})
			if err != nil {
				if offset == 0 {
					writePrivilegedError(w, err)
				}
				return
			}
			if len(result.Data) == 0 && result.More {
				return
			}
			if _, err := w.Write(result.Data); err != nil {
				return
			}
			offset += int64(len(result.Data))
			if !result.More {
				break
			}
		}
	case "verify":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := validateEmptyJSONBody(r); err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := s.backupOperation(r.Context(), privileged.BackupRequest{
			Action: privileged.BackupActionVerify, ArchiveName: name,
		})
		if err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.verify", Target: name, Success: false, Error: err.Error()})
			writePrivilegedError(w, err)
			return
		}
		s.recordRequestAudit(r, audit.Record{Action: "backup.verify", Target: name, Success: true})
		writeJSON(w, backupVerificationFromPrivileged(result))
	case "restore":
		s.queuePanelBackupRestore(w, r, name)
	default:
		writeNotFound(w)
	}
}

func (s *managementState) queuePanelBackupRestore(w http.ResponseWriter, r *http.Request, archiveName string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var request struct {
		Confirm bool `json:"confirm"`
	}
	if !decodeJSONRequest(w, r, &request) {
		return
	}
	if !request.Confirm {
		writeError(w, "restore requires confirm=true", http.StatusBadRequest)
		return
	}
	if !s.beginBackupMutation(w) {
		return
	}
	releaseMutation := true
	defer func() {
		if releaseMutation {
			s.backupMutationMu.Unlock()
		}
	}()
	if _, err := s.backupOperation(r.Context(), privileged.BackupRequest{
		Action: privileged.BackupActionVerify, ArchiveName: archiveName,
	}); err != nil {
		writePrivilegedError(w, err)
		return
	}
	id, err := generateRandomHex(16)
	if err != nil {
		writeError(w, "failed to create restore job", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	ownerSessionToken := currentSessionToken(r)
	job := BackupRestoreJob{
		ID:                id,
		Archive:           archiveName,
		Status:            "queued",
		CreatedAt:         now,
		ownerSessionToken: ownerSessionToken,
	}
	s.backupJobsMu.Lock()
	s.backupJobs[id] = job
	persistErr := s.persistBackupRestoreJobsLocked()
	if persistErr != nil {
		delete(s.backupJobs, id)
	}
	s.backupJobsMu.Unlock()
	if persistErr != nil {
		writeError(w, "failed to persist restore job", http.StatusInternalServerError)
		return
	}
	actor, role := s.auditActor(r)
	ip := clientIP(r)
	userAgent := r.UserAgent()
	releaseMutation = false
	go s.runPanelBackupRestore(id, archiveName, ownerSessionToken, actor, role, ip, userAgent)
	writeJSONStatus(w, http.StatusAccepted, job)
}

func (s *managementState) runPanelBackupRestore(id, name, ownerSessionToken, actor, role, ip, userAgent string) {
	defer s.backupMutationMu.Unlock()
	s.clientRequestMu.Lock()
	defer s.clientRequestMu.Unlock()
	if err := s.updateBackupRestoreJob(id, func(job *BackupRestoreJob) {
		job.Status = "running"
		job.StartedAt = time.Now().UTC()
	}); err != nil {
		s.backupJobsMu.Lock()
		job := s.backupJobs[id]
		job.Status = "failed"
		job.FinishedAt = time.Now().UTC()
		job.Error = "failed to persist restore job state"
		s.backupJobs[id] = job
		_ = s.persistBackupRestoreJobsLocked()
		s.backupJobsMu.Unlock()
		_ = s.appendBackupRestoreAudit(audit.Record{Actor: actor, Role: role, Action: "backup.restore", Target: name, IP: ip, UserAgent: userAgent, Success: false, Error: "persist restore job state"})
		return
	}
	s.mu.Lock()
	s.clientSubsystemStopping = true
	workers := detachClientBackgroundWorkers(s)
	s.mu.Unlock()
	// Wait without s.mu: a reconciler already inside ReconcileOnce may need the
	// mutex to finish its final mutation. The stopping flag prevents any reload
	// in this interval from starting replacement workers.
	stopClientBackgroundWorkers(workers)

	s.mu.Lock()
	closeErr := closeClientDatabase(s)
	var result privileged.BackupResult
	var err error
	if closeErr != nil {
		err = fmt.Errorf("close database for restore: %w", closeErr)
	} else {
		result, err = s.backupOperation(s.lifecycleContext(), privileged.BackupRequest{
			Action: privileged.BackupActionRestore, ArchiveName: name,
		})
	}
	// Reopen on both success and failure. The helper rolls all staged files back
	// on failure, so this reconnects either the restored DB or the original DB.
	s.clientSubsystemStopping = false
	reopenErr := NewManagementStateLifecycle(s).ReloadLocked()
	s.mu.Unlock()
	if err == nil {
		err = reopenErr
	} else if reopenErr != nil {
		err = fmt.Errorf("%v; reopen database: %w", err, reopenErr)
	}
	if err == nil {
		_, err = s.sessionRegistry().DeleteAllExceptPersisted(ownerSessionToken)
	}
	_ = s.appendBackupRestoreAudit(audit.Record{
		Actor:     actor,
		Role:      role,
		Action:    "backup.restore",
		Target:    name,
		IP:        ip,
		UserAgent: userAgent,
		Success:   err == nil,
		Error:     errorString(err),
	})
	finished := time.Now().UTC()
	if err == nil {
		s.scheduleBackupRestoreOwnerSessionRevocation(id, ownerSessionToken)
	}
	persistJobErr := s.updateBackupRestoreJob(id, func(job *BackupRestoreJob) {
		job.FinishedAt = finished
		job.SafetyStatePath = result.SafetyStatePath
		job.SafetyKeyPath = result.SafetyKeyPath
		job.SafetyDatabasePath = result.SafetyDatabasePath
		if err != nil {
			job.Status = "failed"
			job.Error = "restore operation failed"
			return
		}
		job.Status = "succeeded"
	})
	if persistJobErr != nil {
		_ = s.appendBackupRestoreAudit(audit.Record{Actor: actor, Role: role, Action: "backup.restore.job_persist", Target: id, Success: false, Error: "persist restore job state"})
	}
}

func (s *managementState) appendBackupRestoreAudit(record audit.Record) error {
	if s.backupRestoreAudit != nil {
		return s.backupRestoreAudit(record)
	}
	return s.auditRecorder().Append(record)
}

func (s *managementState) handleBackupRestoreJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/backup-restore-jobs/")
	if id == "" || strings.ContainsAny(id, `/\\`) {
		writeError(w, "invalid restore job id", http.StatusBadRequest)
		return
	}
	job, ok := s.backupRestoreJob(id)
	if !ok {
		writeNotFound(w)
		return
	}
	authorized := requestHasAdminRole(s, r)
	if !authorized && job.ownerSessionToken != "" {
		if cookie, err := r.Cookie("veil_session"); err == nil {
			authorized = subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(job.ownerSessionToken)) == 1
		}
	}
	if !authorized {
		writeError(w, "forbidden: restore owner or admin role required", http.StatusForbidden)
		return
	}
	writeJSON(w, job)
	if job.Status == "succeeded" && job.ownerSessionToken != "" {
		s.revokeBackupRestoreOwnerSession(id, job.ownerSessionToken)
	}
}

func (s *managementState) backupRestoreJob(id string) (BackupRestoreJob, bool) {
	s.backupJobsMu.Lock()
	defer s.backupJobsMu.Unlock()
	job, ok := s.backupJobs[id]
	return job, ok
}

func (s *managementState) updateBackupRestoreJob(id string, update func(*BackupRestoreJob)) error {
	s.backupJobsMu.Lock()
	defer s.backupJobsMu.Unlock()
	job, ok := s.backupJobs[id]
	if !ok {
		return fmt.Errorf("restore job not found")
	}
	update(&job)
	s.backupJobs[id] = job
	return s.persistBackupRestoreJobsLocked()
}

func parsePanelBackupPath(r *http.Request) (string, string, bool) {
	escaped := strings.ToLower(r.URL.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") {
		return "", "", false
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/backups/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" ||
		filepath.Base(parts[0]) != parts[0] || strings.ContainsAny(parts[0], `\`) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *managementState) backupOperation(ctx context.Context, request privileged.BackupRequest) (privileged.BackupResult, error) {
	if s.privileged == nil {
		return privileged.BackupResult{}, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper is unavailable",
		}
	}
	return s.privileged.Backup(ctx, request)
}

func backupEntriesFromPrivileged(backupDir string, entries []privileged.BackupArchive) []backup.ArchiveEntry {
	result := make([]backup.ArchiveEntry, 0, len(entries))
	for _, entry := range entries {
		createdAt, _ := time.Parse(time.RFC3339, entry.CreatedAt)
		result = append(result, backup.ArchiveEntry{
			Name: entry.Name, Path: filepath.Join(backupDir, entry.Name), Size: entry.Size, CreatedAt: createdAt, Encrypted: entry.Encrypted,
		})
	}
	return result
}
