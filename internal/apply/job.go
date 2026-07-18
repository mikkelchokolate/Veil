// Package apply implements the reliable apply workflow: durable desired/applied
// revisions, persistent apply jobs, and the orchestration that renders an
// immutable revision snapshot into runtime configuration with health checks
// and rollback. It layers on top of the existing staged-apply mechanics
// (internal/applyflow) without replacing their validation, promotion, or
// rollback behaviour.
package apply

import "time"

// Job status values. A job moves forward through these states; terminal states
// are StatusSucceeded, StatusFailed, StatusRolledBack, and StatusRollbackFailed.
const (
	StatusPending        = "pending"
	StatusPlanning       = "planning"
	StatusValidating     = "validating"
	StatusApplying       = "applying"
	StatusHealthCheck    = "health_check"
	StatusSucceeded      = "succeeded"
	StatusFailed         = "failed"
	StatusRollingBack    = "rolling_back"
	StatusRolledBack     = "rolled_back"
	StatusRollbackFailed = "rollback_failed"
)

// System states derived from desired/applied revisions and the latest job.
const (
	StateSynced      = "synced"
	StatePending     = "pending"
	StateApplying    = "applying"
	StateFailed      = "failed"
	StateRollingBack = "rolling_back"
	StateRolledBack  = "rolled_back"
	StateDegraded    = "degraded"
)

// OperationResult records one concrete runtime change attempted by a job.
type OperationResult struct {
	Type    string `json:"type"`
	Target  string `json:"target,omitempty"`
	Success bool   `json:"success"`
	Detail  string `json:"detail,omitempty"`
}

// Job is a durable record of one apply attempt against a specific immutable
// desired revision. Jobs are never rewritten after reaching a terminal state;
// a retry creates a new job that references the same or a newer revision.
type Job struct {
	ID              string            `json:"id"`
	DesiredRevision uint64            `json:"desiredRevision"`
	BaseRevision    uint64            `json:"baseRevision"`
	Status          string            `json:"status"`
	Trigger         string            `json:"trigger"`
	ActorID         string            `json:"actorId,omitempty"`
	CreatedAt       int64             `json:"createdAt"`
	StartedAt       *int64            `json:"startedAt,omitempty"`
	FinishedAt      *int64            `json:"finishedAt,omitempty"`
	ErrorCode       string            `json:"errorCode,omitempty"`
	ErrorMessage    string            `json:"errorMessage,omitempty"`
	Operations      []OperationResult `json:"operations,omitempty"`
}

// Terminal reports whether the job has finished (successfully or not).
func (j Job) Terminal() bool {
	switch j.Status {
	case StatusSucceeded, StatusFailed, StatusRolledBack, StatusRollbackFailed:
		return true
	}
	return false
}

// Active reports whether the job is still running or queued.
func (j Job) Active() bool { return !j.Terminal() }

// Revisions is the current desired/applied revision pair.
type Revisions struct {
	Desired uint64 `json:"desired"`
	Applied uint64 `json:"applied"`
}

// nowUnix returns the current time in Unix seconds (seam for tests).
var nowUnix = func() int64 { return time.Now().Unix() }
