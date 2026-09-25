package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	versionflow "github.com/mikkelchokolate/Veil/internal/cliflow/version"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type panelUpdateJob struct {
	ID                string `json:"id"`
	Version           string `json:"version"`
	Status            string `json:"status"`
	StageApplyJobID   string `json:"stageApplyJobId,omitempty"`
	RestartApplyJobID string `json:"restartApplyJobId,omitempty"`
	Error             string `json:"error,omitempty"`
	CreatedAt         int64  `json:"createdAt"`
	UpdatedAt         int64  `json:"updatedAt"`
}

func (s *managementState) createPanelUpdateJob(version string) (panelUpdateJob, error) {
	now := time.Now().UTC().Unix()
	job := panelUpdateJob{ID: uuid.NewString(), Version: version, Status: "staging", CreatedAt: now, UpdatedAt: now}
	_, err := s.db.Exec(`INSERT INTO panel_update_jobs(id,target_version,status,created_at,updated_at) VALUES(?,?,?,?,?)`,
		job.ID, job.Version, job.Status, now, now)
	return job, err
}

// panelUpdateJobRetentionSeconds bounds how long terminal update jobs are
// kept before reconcile prunes them; the table otherwise grows without bound.
const panelUpdateJobRetentionSeconds int64 = 30 * 24 * 60 * 60

// updatePanelUpdateJob persists a durable status transition. The error is
// returned so callers can fail the request (or at least log) instead of
// leaving the SPA polling a stale status forever.
func (s *managementState) updatePanelUpdateJob(id, status, stageJobID, restartJobID string, operationErr error) error {
	message := ""
	if operationErr != nil {
		message = operationErr.Error()
		if len(message) > 1024 {
			message = message[:1024]
		}
	}
	_, err := s.db.Exec(`UPDATE panel_update_jobs SET status=?,stage_apply_job_id=CASE WHEN ?<>'' THEN ? ELSE stage_apply_job_id END,
 restart_apply_job_id=CASE WHEN ?<>'' THEN ? ELSE restart_apply_job_id END,error_message=?,updated_at=? WHERE id=?`,
		status, stageJobID, stageJobID, restartJobID, restartJobID, message, time.Now().UTC().Unix(), id)
	return err
}

func (s *managementState) getPanelUpdateJob(id string) (panelUpdateJob, error) {
	var job panelUpdateJob
	err := s.db.QueryRow(`SELECT id,target_version,status,stage_apply_job_id,restart_apply_job_id,error_message,created_at,updated_at
FROM panel_update_jobs WHERE id=?`, id).Scan(&job.ID, &job.Version, &job.Status, &job.StageApplyJobID,
		&job.RestartApplyJobID, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

func (s *managementState) reconcilePanelUpdateJobs(runningVersion string) {
	now := time.Now().UTC().Unix()
	// Retention: terminal jobs older than the window are deleted so the table
	// does not grow across updates.
	if _, err := s.db.Exec(`DELETE FROM panel_update_jobs WHERE status IN ('succeeded','failed') AND updated_at<?`, now-panelUpdateJobRetentionSeconds); err != nil {
		log.Printf("panel update jobs: prune terminal history: %v", err)
	}
	rows, err := s.db.Query(`SELECT id,target_version,status,updated_at FROM panel_update_jobs WHERE status IN ('staging','restart_pending','restarting')`)
	if err != nil {
		return
	}
	pending, err := collectPendingUpdateJobs(rows)
	if err != nil {
		// A partially iterated result must not be reconciled: jobs skipped by
		// a mid-iteration failure would stay in-flight forever (#1063).
		rows.Close()
		log.Printf("panel update jobs: read pending jobs: %v", err)
		return
	}
	// The pool holds a single connection; updates must run after the cursor
	// closes or they wait on the conn the scan still occupies.
	if closeErr := rows.Close(); closeErr != nil {
		log.Printf("panel update jobs: close pending jobs cursor: %v", closeErr)
		return
	}
	for _, job := range pending {
		var updateErr error
		switch {
		case job.status == "staging":
			// A staging row can only be written by a process that has since
			// exited: the install runs inside the update request, so anything
			// left here at startup is an interrupted update, not a live one.
			updateErr = s.updatePanelUpdateJob(job.id, "failed", "", "", errors.New("panel update was interrupted before the staged version was installed"))
		case versionflow.ReleaseTag(job.version) == versionflow.ReleaseTag(runningVersion):
			updateErr = s.updatePanelUpdateJob(job.id, "succeeded", "", "", nil)
		case now-job.updated > 300:
			updateErr = s.updatePanelUpdateJob(job.id, "failed", "", "", fmt.Errorf("panel restarted without expected version %s", job.version))
		}
		if updateErr != nil {
			log.Printf("panel update job %s: reconcile status failed: %v", job.id, updateErr)
		}
	}
}

// pendingUpdateJob is one in-flight panel update row awaiting reconcile.
type pendingUpdateJob struct {
	id, version, status string
	updated             int64
}

// collectPendingUpdateJobs drains rows into a slice, surfacing both Scan and
// mid-iteration errors instead of silently accepting a partial result.
func collectPendingUpdateJobs(rows *sql.Rows) ([]pendingUpdateJob, error) {
	var pending []pendingUpdateJob
	for rows.Next() {
		var job pendingUpdateJob
		if err := rows.Scan(&job.id, &job.version, &job.status, &job.updated); err != nil {
			return nil, err
		}
		pending = append(pending, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pending, nil
}

func (s *managementState) installPanelUpdate(ctx context.Context, version string) (privileged.UpdateResult, veilapply.Job, error) {
	if !s.applyTrackingEnabled() || s.applyRunner == nil {
		return privileged.UpdateResult{}, veilapply.Job{}, errors.New("durable apply runner is unavailable")
	}
	revision, err := s.ensureRunnableRevision()
	if err != nil {
		return privileged.UpdateResult{}, veilapply.Job{}, err
	}
	var updateResult privileged.UpdateResult
	job, runErr := s.applyRunner.RunOperationContext(ctx, revision, "panel-update-install", "admin",
		veilapply.ContextExecutorFunc(func(operationContext context.Context, pinnedRevision uint64) (veilapply.Result, error) {
			result, err := s.convergeRevisionForSideEffect(operationContext, pinnedRevision)
			if err != nil {
				return result, err
			}
			fence, ok := veilapply.FenceFromContext(operationContext)
			if !ok {
				return veilapply.Result{ErrorCode: "fence_missing"}, errors.New("update fence unavailable")
			}
			if err := veilapply.MarkSideEffectStarting(operationContext, veilapply.PublicationDetails{
				ServicePhase: "update-install", UpdateTransactionID: fence.OperationID,
				TargetVersion: version, ActivationManifest: "/usr/local/bin/.veil-update-evidence.json",
			}); err != nil {
				return veilapply.Result{ErrorCode: "publication_intent"}, err
			}
			updateResult, err = s.privileged.StageUpdate(operationContext, privileged.UpdateRequest{
				ArtifactID: "veil-update", Version: version,
				Fence: privileged.FenceToken{Owner: fence.Owner, Generation: fence.Generation,
					OperationID: fence.OperationID, LeaseExpiresAt: fence.LeaseExpiresAt},
			})
			if err == nil {
				err = completeSideEffectPublication(operationContext, veilapply.PublicationDetails{
					Artifacts: []string{"veil-update"}, ServicePhase: "update-install",
					UpdateTransactionID: updateResult.TransactionID, ExpectedBinaryDigest: updateResult.ExpectedDigest,
					OldBinaryDigest: updateResult.OldDigest, InstalledInode: updateResult.InstalledInode,
					TargetVersion: updateResult.Version, ActivationManifest: updateResult.ActivationManifest, CommitPhase: updateResult.CommitPhase,
				})
			}
			operation := veilapply.OperationResult{Type: "panel-update-install", Target: version, Success: err == nil}
			if err != nil {
				operation.Detail = err.Error()
			}
			result.Success = err == nil
			result.Operations = append(result.Operations, operation)
			result.ErrorCode = "update_install"
			return result, err
		}))
	return updateResult, job, runErr
}

func (s *managementState) restartPanelForUpdate(updateJobID string) {
	defer s.endPanelUpdate()
	if !s.applyTrackingEnabled() || s.applyRunner == nil {
		if err := s.updatePanelUpdateJob(updateJobID, "failed", "", "", errors.New("durable apply runner is unavailable")); err != nil {
			log.Printf("panel update job %s: record failure status: %v", updateJobID, err)
		}
		return
	}
	revision, err := s.ensureRunnableRevision()
	if err != nil {
		if updateErr := s.updatePanelUpdateJob(updateJobID, "failed", "", "", err); updateErr != nil {
			log.Printf("panel update job %s: record failure status: %v", updateJobID, updateErr)
		}
		return
	}
	job, runErr := s.applyRunner.RunOperationContext(s.lifecycleContext(), revision, "panel-update-restart", "system",
		veilapply.ContextExecutorFunc(func(operationContext context.Context, pinnedRevision uint64) (veilapply.Result, error) {
			result, err := s.convergeRevisionForSideEffect(operationContext, pinnedRevision)
			if err != nil {
				return result, err
			}
			fence, ok := veilapply.FenceFromContext(operationContext)
			if !ok {
				return veilapply.Result{ErrorCode: "fence_missing"}, errors.New("restart fence unavailable")
			}
			if err := veilapply.MarkSideEffectStarting(operationContext, veilapply.PublicationDetails{
				ServicePhase: "restart-panel", UpdateTransactionID: fence.OperationID,
				ActivationManifest: "/usr/local/bin/.veil-restart-evidence.json",
			}); err != nil {
				return veilapply.Result{ErrorCode: "publication_intent"}, err
			}
			operationContext = privileged.ContextWithRestartPanelRequest(operationContext, privileged.RestartPanelRequest{Fence: privileged.FenceToken{
				Owner: fence.Owner, Generation: fence.Generation, OperationID: fence.OperationID, LeaseExpiresAt: fence.LeaseExpiresAt,
			}})
			err = s.privileged.RestartPanel(operationContext)
			if err == nil {
				err = completeSideEffectPublication(operationContext, veilapply.PublicationDetails{
					Artifacts: []string{"veil.service"}, ServicePhase: "restart-panel",
					UpdateTransactionID: fence.OperationID, ActivationManifest: "/usr/local/bin/.veil-restart-evidence.json",
				})
			}
			operation := veilapply.OperationResult{Type: "panel-update-restart", Target: "veil.service", Success: err == nil}
			if err != nil {
				operation.Detail = err.Error()
			}
			result.Success = err == nil
			result.Operations = append(result.Operations, operation)
			result.ErrorCode = "restart_failed"
			return result, err
		}))
	if runErr != nil {
		if err := s.updatePanelUpdateJob(updateJobID, "failed", "", job.ID, runErr); err != nil {
			log.Printf("panel update job %s: record failure status: %v", updateJobID, err)
		}
		return
	}
	if err := s.updatePanelUpdateJob(updateJobID, "restarting", "", job.ID, nil); err != nil {
		log.Printf("panel update job %s: record restarting status: %v", updateJobID, err)
	}
}

func (routes PanelRoutes) handlePanelUpdateJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/version/update/jobs/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, "invalid update job ID", http.StatusBadRequest)
		return
	}
	job, err := routes.State.getPanelUpdateJob(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, "update job not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, "read update job", http.StatusInternalServerError)
		return
	}
	if requestIsViewer(r) {
		// The stored error text can embed privileged subprocess output from
		// the apply runner; viewers get a redacted copy (#1065).
		job.Error = sanitizeServiceLogOutput(job.Error)
	}
	writeJSON(w, job)
}
