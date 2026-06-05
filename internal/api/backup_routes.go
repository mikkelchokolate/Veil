package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
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
		entries, err := backup.ListArchives(s.backupDir)
		if err != nil {
			writeError(w, "failed to list backup archives", http.StatusInternalServerError)
			return
		}
		writeJSON(w, entries)
	case http.MethodPost:
		var request backupCreateRequest
		if !decodeJSONRequest(w, r, &request) {
			return
		}
		passphrase, err := s.panelBackupPassphrase()
		if err != nil {
			writeError(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		s.mu.Lock()
		statePath := s.statePath
		keyPath := s.keyPath
		version := s.version
		s.mu.Unlock()
		data, err := backup.CreateBackupWithOptions(statePath, keyPath, passphrase, backup.ArchiveOptions{
			VeilVersion: version,
		})
		if err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.create", Target: "state", Success: false, Error: err.Error()})
			writeError(w, "failed to create backup archive", http.StatusInternalServerError)
			return
		}
		verification, err := backup.VerifyBackup(data, passphrase)
		if err != nil {
			writeError(w, "generated backup failed verification", http.StatusInternalServerError)
			return
		}
		name, err := nextPanelBackupName(s.backupDir, time.Now().UTC())
		if err != nil {
			writeError(w, "failed to allocate backup archive name", http.StatusInternalServerError)
			return
		}
		path := filepath.Join(s.backupDir, name)
		if err := atomicfile.Write(path, data, 0o600, 0o700); err != nil {
			writeError(w, "failed to persist backup archive", http.StatusInternalServerError)
			return
		}
		entry, ok, err := findPanelBackup(s.backupDir, name)
		if err != nil || !ok {
			writeError(w, "failed to inspect created backup archive", http.StatusInternalServerError)
			return
		}
		response := BackupCreateResponse{Archive: entry, Verification: verification}
		if request.Prune {
			policy := retentionFromRequest(request.Daily, request.Weekly, request.Monthly)
			result, err := backup.PruneArchives(s.backupDir, policy, false)
			if err != nil {
				writeError(w, "backup created but retention failed", http.StatusInternalServerError)
				return
			}
			response.Prune = &result
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
	result, err := backup.PruneArchives(
		s.backupDir,
		retentionFromRequest(request.Daily, request.Weekly, request.Monthly),
		false,
	)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.recordRequestAudit(r, audit.Record{
		Action:  "backup.prune",
		Target:  s.backupDir,
		Success: true,
		Details: map[string]any{"deleted": len(result.Deleted), "kept": len(result.Kept)},
	})
	writeJSON(w, result)
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
	entry, found, err := findPanelBackup(s.backupDir, name)
	if err != nil {
		writeError(w, "failed to inspect backup archive", http.StatusInternalServerError)
		return
	}
	if !found {
		writeNotFound(w)
		return
	}
	switch action {
	case "download":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, entry.Name))
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, entry.Path)
	case "verify":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := validateEmptyJSONBody(r); err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		report, err := s.verifyPanelBackup(entry.Path)
		if err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.verify", Target: name, Success: false, Error: err.Error()})
			writeError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		s.recordRequestAudit(r, audit.Record{Action: "backup.verify", Target: name, Success: true})
		writeJSON(w, report)
	case "restore":
		s.queuePanelBackupRestore(w, r, entry)
	default:
		writeNotFound(w)
	}
}

func (s *managementState) queuePanelBackupRestore(w http.ResponseWriter, r *http.Request, entry backup.ArchiveEntry) {
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
	passphrase, err := s.panelBackupPassphrase()
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	data, err := readPanelBackup(entry.Path)
	if err != nil {
		writeError(w, "failed to read backup archive", http.StatusInternalServerError)
		return
	}
	if _, err := backup.VerifyBackup(data, passphrase); err != nil {
		writeError(w, err.Error(), http.StatusUnprocessableEntity)
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
		Archive:           entry.Name,
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
	go s.runPanelBackupRestore(id, entry.Name, data, passphrase, ownerSessionToken, actor, role, ip, userAgent)
	writeJSONStatus(w, http.StatusAccepted, job)
}

func (s *managementState) runPanelBackupRestore(id, name string, data []byte, passphrase, ownerSessionToken, actor, role, ip, userAgent string) {
	s.updateBackupRestoreJob(id, func(job *BackupRestoreJob) {
		job.Status = "running"
		job.StartedAt = time.Now().UTC()
	})
	result, err := backup.RestoreBackupWithOptions(data, s.statePath, s.keyPath, passphrase, backup.RestoreOptions{})
	if err == nil {
		err = s.Reload()
	}
	if err == nil {
		_, err = s.sessionRegistry().DeleteAllExcept(ownerSessionToken)
	}
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
	_ = s.auditRecorder().Append(audit.Record{
		Actor:     actor,
		Role:      role,
		Action:    "backup.restore",
		Target:    name,
		IP:        ip,
		UserAgent: userAgent,
		Success:   err == nil,
		Error:     errorString(err),
	})
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
	id := strings.TrimPrefix(r.URL.Path, "/api/backups/restore-jobs/")
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

func (s *managementState) verifyPanelBackup(path string) (backup.VerificationReport, error) {
	passphrase, err := s.panelBackupPassphrase()
	if err != nil {
		return backup.VerificationReport{}, err
	}
	data, err := readPanelBackup(path)
	if err != nil {
		return backup.VerificationReport{}, err
	}
	return backup.VerifyBackup(data, passphrase)
}

func (s *managementState) panelBackupPassphrase() (string, error) {
	body, err := os.ReadFile(s.backupPassphrasePath)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("scheduled backup passphrase is not configured; run `veil backup schedule enable`")
	}
	if err != nil {
		return "", fmt.Errorf("read backup passphrase: %w", err)
	}
	passphrase := strings.TrimRight(string(body), "\r\n")
	if len(passphrase) < 16 {
		return "", errors.New("configured backup passphrase is too short")
	}
	return passphrase, nil
}

func readPanelBackup(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxPanelBackupBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxPanelBackupBytes {
		return nil, errors.New("backup archive exceeds Panel size limit")
	}
	return body, nil
}

func findPanelBackup(dir, name string) (backup.ArchiveEntry, bool, error) {
	entries, err := backup.ListArchives(dir)
	if err != nil {
		return backup.ArchiveEntry{}, false, err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return backup.ArchiveEntry{}, false, nil
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

func nextPanelBackupName(dir string, now time.Time) (string, error) {
	for offset := 0; offset < 60; offset++ {
		name := "veil_backup_" + now.Add(time.Duration(offset)*time.Second).Format("20060102_150405") + ".tar.gz.enc"
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			return name, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("too many backup archives created within one minute")
}
