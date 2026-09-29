package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// SFTP remote-backup destination endpoints. The destination secrets live in
// a root-only file under the etc dir that the unprivileged panel process can
// neither read nor write, so every operation here is proxied to the
// privileged helper (or the in-process adapter in dev/test). The API never
// echoes secret values: PUT treats password/keyPassphrase/hostKey as
// write-only and GET returns *Set flags instead.

// BackupSftpDestinationResponse flattens the secret-free destination view
// and attaches the recorded remote-operation status.
type BackupSftpDestinationResponse struct {
	privileged.BackupSftpDestination
	Status privileged.BackupSftpStatus `json:"status"`
}

type backupSftpFetchRequest struct {
	Name string `json:"name"`
}

func (s *managementState) backupSftpOperation(ctx context.Context, request privileged.BackupSftpRequest) (privileged.BackupSftpResult, error) {
	if s.privileged == nil {
		return privileged.BackupSftpResult{}, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper is unavailable",
		}
	}
	operator, ok := s.privileged.(privileged.BackupSftpOperator)
	if !ok {
		return privileged.BackupSftpResult{}, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper does not support sftp destinations",
		}
	}
	return operator.BackupSftp(ctx, request)
}

// handleBackupSftp serves /api/backups/sftp — the exact-match pattern sits
// ahead of the /api/backups/{name} subtree, and "sftp" can never collide with
// a real archive name because managed archives end in .tar.gz[.enc].
func (s *managementState) handleBackupSftp(w http.ResponseWriter, r *http.Request) {
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := s.backupSftpOperation(r.Context(), privileged.BackupSftpRequest{Action: privileged.BackupSftpActionGet})
		if err != nil {
			writePrivilegedError(w, err)
			return
		}
		writeJSON(w, backupSftpDestinationResponse(result))
	case http.MethodPut:
		var request privileged.BackupSftpConfig
		if !decodeJSONRequest(w, r, &request) {
			return
		}
		fence, releaseFence, fenceErr := s.acquireRuntimeFence("backup-sftp-configure")
		if fenceErr != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.sftp.configure", Target: request.Host, Success: false, Error: fenceErr.Error()})
			writeError(w, "sftp destination fencing lease is unavailable: "+fenceErr.Error(), http.StatusConflict)
			return
		}
		defer releaseFence()
		result, err := s.backupSftpOperation(r.Context(), privileged.BackupSftpRequest{
			Action: privileged.BackupSftpActionSet, Config: &request, Fence: fence,
		})
		if err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.sftp.configure", Target: request.Host, Success: false, Error: err.Error()})
			writePrivilegedError(w, err)
			return
		}
		// Audit only the non-secret destination coordinates — never the
		// password, key passphrase, or host key material from the request.
		s.recordRequestAudit(r, audit.Record{
			Action:  "backup.sftp.configure",
			Target:  request.Host,
			Success: true,
			Details: map[string]any{
				"enabled": request.Enabled, "port": request.Port, "user": request.User,
				"remoteDir": request.RemoteDir, "authType": request.AuthType,
			},
		})
		writeJSON(w, backupSftpDestinationResponse(result))
	case http.MethodDelete:
		fence, releaseFence, fenceErr := s.acquireRuntimeFence("backup-sftp-delete")
		if fenceErr != nil {
			writeError(w, "sftp destination fencing lease is unavailable: "+fenceErr.Error(), http.StatusConflict)
			return
		}
		defer releaseFence()
		if _, err := s.backupSftpOperation(r.Context(), privileged.BackupSftpRequest{
			Action: privileged.BackupSftpActionDelete, Fence: fence,
		}); err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.sftp.delete", Target: "destination", Success: false, Error: err.Error()})
			writePrivilegedError(w, err)
			return
		}
		s.recordRequestAudit(r, audit.Record{Action: "backup.sftp.delete", Target: "destination", Success: true})
		writeJSON(w, map[string]any{"configured": false})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

// handleBackupSftpRemote serves /api/backups/sftp/<sub>: the remote archive
// listing and the fetch operation that materializes one remote archive into
// the local backup dir for a normal restore.
func (s *managementState) handleBackupSftpRemote(w http.ResponseWriter, r *http.Request) {
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/backups/sftp/")
	switch rest {
	case "remote":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		result, err := s.backupSftpOperation(r.Context(), privileged.BackupSftpRequest{Action: privileged.BackupSftpActionList})
		if err != nil {
			writePrivilegedError(w, err)
			return
		}
		writeJSON(w, backupEntriesFromPrivileged(s.backupDir, result.Archives))
	case "fetch":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var request backupSftpFetchRequest
		if !decodeJSONRequest(w, r, &request) {
			return
		}
		if !s.beginBackupMutation(w) {
			return
		}
		defer s.backupMutationMu.Unlock()
		fence, releaseFence, fenceErr := s.acquireRuntimeFence("backup-sftp-fetch")
		if fenceErr != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.sftp.fetch", Target: request.Name, Success: false, Error: fenceErr.Error()})
			writeError(w, "sftp fetch fencing lease is unavailable: "+fenceErr.Error(), http.StatusConflict)
			return
		}
		defer releaseFence()
		result, err := s.backupSftpOperation(r.Context(), privileged.BackupSftpRequest{
			Action: privileged.BackupSftpActionFetch, ArchiveName: request.Name, Fence: fence,
		})
		if err != nil {
			s.recordRequestAudit(r, audit.Record{Action: "backup.sftp.fetch", Target: request.Name, Success: false, Error: err.Error()})
			writePrivilegedError(w, err)
			return
		}
		s.recordRequestAudit(r, audit.Record{Action: "backup.sftp.fetch", Target: request.Name, Success: true})
		entry := backup.ArchiveEntry{}
		if result.Archive != nil {
			entry = backup.ArchiveEntry{
				Name:      result.Archive.Name,
				Path:      filepath.Join(s.backupDir, result.Archive.Name),
				Size:      result.Archive.Size,
				Encrypted: result.Archive.Encrypted,
			}
			if createdAt, parseErr := time.Parse(time.RFC3339, result.Archive.CreatedAt); parseErr == nil {
				entry.CreatedAt = createdAt
			}
		}
		writeJSON(w, entry)
	default:
		writeNotFound(w)
	}
}

func backupSftpDestinationResponse(result privileged.BackupSftpResult) BackupSftpDestinationResponse {
	response := BackupSftpDestinationResponse{}
	if result.Destination != nil {
		response.BackupSftpDestination = *result.Destination
	}
	if result.Status != nil {
		response.Status = *result.Status
	}
	return response
}
