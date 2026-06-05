package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

const maxPanelBackupBytes int64 = 64 * 1024 * 1024

type BackupCreateResponse struct {
	Archive      backup.ArchiveEntry       `json:"archive"`
	Verification backup.VerificationReport `json:"verification"`
	Prune        *backup.PruneResult       `json:"prune,omitempty"`
}

type BackupRestoreJob struct {
	ID                string    `json:"id"`
	Archive           string    `json:"archive"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
	StartedAt         time.Time `json:"startedAt,omitempty"`
	FinishedAt        time.Time `json:"finishedAt,omitempty"`
	Error             string    `json:"error,omitempty"`
	SafetyStatePath   string    `json:"safetyStatePath,omitempty"`
	SafetyKeyPath     string    `json:"safetyKeyPath,omitempty"`
	ownerSessionToken string
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
		writeJSON(w, backupEntriesFromPrivileged(result.Archives))
	case http.MethodPost:
		var request backupCreateRequest
		if !decodeJSONRequest(w, r, &request) {
			return
		}
		result, err := s.backupOperation(r.Context(), privileged.BackupRequest{Action: privileged.BackupActionCreate})
		if err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.create", Target: "state", Success: false, Error: err.Error()})
			writePrivilegedError(w, err)
			return
		}
		name := result.ArchiveName
		response := BackupCreateResponse{
			Archive: backup.ArchiveEntry{Name: name, CreatedAt: time.Now().UTC(), Encrypted: true},
			Verification: backup.VerificationReport{
				Encrypted: result.Verified,
			},
		}
		if request.Prune {
			pruned, err := s.backupOperation(r.Context(), privileged.BackupRequest{
				Action: privileged.BackupActionPrune, Daily: request.Daily, Weekly: request.Weekly, Monthly: request.Monthly,
			})
			if err != nil {
				writePrivilegedError(w, err)
				return
			}
			response.Prune = &backup.PruneResult{Deleted: pruned.Pruned, Kept: pruned.Kept}
		}
		s.recordRequestAudit(r, audit.Record{
			Action:  "backup.create",
			Target:  name,
			Success: true,
			Details: map[string]any{"prune": request.Prune},
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
		result, err := s.backupOperation(r.Context(), privileged.BackupRequest{
			Action: privileged.BackupActionRead, ArchiveName: name,
		})
		if err != nil {
			writePrivilegedError(w, err)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(result.Data)
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
		writeJSON(w, backup.VerificationReport{Encrypted: result.Verified})
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
	s.backupJobsMu.Unlock()
	actor, role := s.auditActor(r)
	ip := clientIP(r)
	userAgent := r.UserAgent()
	go s.runPanelBackupRestore(id, archiveName, ownerSessionToken, actor, role, ip, userAgent)
	writeJSONStatus(w, http.StatusAccepted, job)
}

func (s *managementState) runPanelBackupRestore(id, name, ownerSessionToken, actor, role, ip, userAgent string) {
	s.updateBackupRestoreJob(id, func(job *BackupRestoreJob) {
		job.Status = "running"
		job.StartedAt = time.Now().UTC()
	})
	result, err := s.backupOperation(context.Background(), privileged.BackupRequest{
		Action: privileged.BackupActionRestore, ArchiveName: name,
	})
	if err == nil {
		err = s.Reload()
	}
	if err == nil {
		_, err = s.sessionRegistry().DeleteAllExcept(ownerSessionToken)
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
	s.updateBackupRestoreJob(id, func(job *BackupRestoreJob) {
		job.FinishedAt = finished
		job.SafetyStatePath = result.SafetyStatePath
		job.SafetyKeyPath = result.SafetyKeyPath
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			return
		}
		job.Status = "succeeded"
	})
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
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/backup-restore-jobs/")
	if id == "" || strings.ContainsAny(id, `/\`) {
		writeError(w, "invalid restore job id", http.StatusBadRequest)
		return
	}
	job, ok := s.backupRestoreJob(id)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, job)
	if job.Status == "succeeded" && job.ownerSessionToken != "" {
		s.sessionRegistry().Delete(job.ownerSessionToken)
	}
}

func (s *managementState) backupRestoreJob(id string) (BackupRestoreJob, bool) {
	s.backupJobsMu.Lock()
	defer s.backupJobsMu.Unlock()
	job, ok := s.backupJobs[id]
	return job, ok
}

func (s *managementState) updateBackupRestoreJob(id string, update func(*BackupRestoreJob)) {
	s.backupJobsMu.Lock()
	defer s.backupJobsMu.Unlock()
	job := s.backupJobs[id]
	update(&job)
	s.backupJobs[id] = job
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

func retentionFromRequest(daily, weekly, monthly int) backup.RetentionPolicy {
	if daily == 0 && weekly == 0 && monthly == 0 {
		return backup.DefaultRetentionPolicy()
	}
	return backup.RetentionPolicy{Daily: daily, Weekly: weekly, Monthly: monthly}
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

func backupEntriesFromPrivileged(entries []privileged.BackupArchive) []backup.ArchiveEntry {
	result := make([]backup.ArchiveEntry, 0, len(entries))
	for _, entry := range entries {
		createdAt, _ := time.Parse(time.RFC3339, entry.CreatedAt)
		result = append(result, backup.ArchiveEntry{
			Name: entry.Name, Size: entry.Size, CreatedAt: createdAt, Encrypted: entry.Encrypted,
		})
	}
	return result
}
