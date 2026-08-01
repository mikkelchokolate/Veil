package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
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

func (s *managementState) updatePanelUpdateJob(id, status, stageJobID, restartJobID string, operationErr error) {
	message := ""
	if operationErr != nil {
		message = operationErr.Error()
		if len(message) > 1024 {
			message = message[:1024]
		}
	}
	_, _ = s.db.Exec(`UPDATE panel_update_jobs SET status=?,stage_apply_job_id=CASE WHEN ?<>'' THEN ? ELSE stage_apply_job_id END,
 restart_apply_job_id=CASE WHEN ?<>'' THEN ? ELSE restart_apply_job_id END,error_message=?,updated_at=? WHERE id=?`,
		status, stageJobID, stageJobID, restartJobID, restartJobID, message, time.Now().UTC().Unix(), id)
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
	rows, err := s.db.Query(`SELECT id,target_version,updated_at FROM panel_update_jobs WHERE status IN ('restart_pending','restarting')`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, version string
		var updated int64
		if rows.Scan(&id, &version, &updated) != nil {
			continue
		}
		switch {
		case version == runningVersion:
			s.updatePanelUpdateJob(id, "succeeded", "", "", nil)
		case now-updated > 300:
			s.updatePanelUpdateJob(id, "failed", "", "", fmt.Errorf("panel restarted without expected version %s", version))
		}
	}
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
			if err := veilapply.MarkRuntimeMutationStarting(operationContext, veilapply.PublicationDetails{ServicePhase: "update-install"}); err != nil {
				return veilapply.Result{ErrorCode: "publication_intent"}, err
			}
			fence, ok := veilapply.FenceFromContext(operationContext)
			if !ok {
				return veilapply.Result{ErrorCode: "fence_missing"}, errors.New("update fence unavailable")
			}
			updateResult, err = s.privileged.StageUpdate(operationContext, privileged.UpdateRequest{
				ArtifactID: "veil-update", Version: version,
				Fence: privileged.FenceToken{Owner: fence.Owner, Generation: fence.Generation,
					OperationID: fence.OperationID, LeaseExpiresAt: fence.LeaseExpiresAt},
			})
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
		s.updatePanelUpdateJob(updateJobID, "failed", "", "", errors.New("durable apply runner is unavailable"))
		return
	}
	revision, err := s.ensureRunnableRevision()
	if err != nil {
		s.updatePanelUpdateJob(updateJobID, "failed", "", "", err)
		return
	}
	job, runErr := s.applyRunner.RunOperationContext(context.Background(), revision, "panel-update-restart", "system",
		veilapply.ContextExecutorFunc(func(operationContext context.Context, pinnedRevision uint64) (veilapply.Result, error) {
			result, err := s.convergeRevisionForSideEffect(operationContext, pinnedRevision)
			if err != nil {
				return result, err
			}
			if err := veilapply.MarkRuntimeMutationStarting(operationContext, veilapply.PublicationDetails{ServicePhase: "restart-panel"}); err != nil {
				return veilapply.Result{ErrorCode: "publication_intent"}, err
			}
			fence, ok := veilapply.FenceFromContext(operationContext)
			if !ok {
				return veilapply.Result{ErrorCode: "fence_missing"}, errors.New("restart fence unavailable")
			}
			operationContext = privileged.ContextWithRestartPanelRequest(operationContext, privileged.RestartPanelRequest{Fence: privileged.FenceToken{
				Owner: fence.Owner, Generation: fence.Generation, OperationID: fence.OperationID, LeaseExpiresAt: fence.LeaseExpiresAt,
			}})
			err = s.privileged.RestartPanel(operationContext)
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
		s.updatePanelUpdateJob(updateJobID, "failed", "", job.ID, runErr)
		return
	}
	s.updatePanelUpdateJob(updateJobID, "restarting", "", job.ID, nil)
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
	writeJSON(w, job)
}
