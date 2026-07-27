package apply

import (
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// ErrApplyBusy is returned when another apply job is already active. The apply
// workflow is serialized: only one job mutates runtime at a time.
var (
	ErrApplyBusy        = errors.New("apply: another apply job is active")
	ErrStaleRevision    = errors.New("apply: revision is not current desired")
	ErrRevisionRollback = errors.New("apply: revision is below current applied")
)

// Result is the outcome of executing the underlying staged-apply pipeline for
// one revision. Success is true only when the configuration was rendered,
// promoted, services reloaded, and health checks passed.
type Result struct {
	Success      bool
	RolledBack   bool
	ErrorCode    string
	ErrorMessage string
	Operations   []OperationResult
}

// ExecuteFunc applies one immutable desired revision to the runtime and
// reports the outcome. It is the seam over the existing staged-apply pipeline
// (applyflow). The function must apply exactly the revision it is given.
type ExecuteFunc func(revision uint64) (Result, error)

// Runner orchestrates apply jobs: it persists a durable job record, runs the
// executor for the pinned revision, and advances applied_revision only on
// success. A Runner is safe for concurrent use; jobs are serialized.
type Runner struct {
	revisions *RevisionStore
	jobs      *JobStore
	execute   ExecuteFunc

	mu     sync.Mutex
	active bool
}

func NewRunner(rs *RevisionStore, js *JobStore, execute ExecuteFunc) *Runner {
	return &Runner{revisions: rs, jobs: js, execute: execute}
}

// Run creates and executes a job for the given desired revision. It returns
// the final job record. A non-nil error indicates the job failed (the durable
// record still reflects the terminal state); ErrApplyBusy indicates a
// concurrent apply is in progress.
func (r *Runner) Run(revision uint64, trigger, actor string) (Job, error) {
	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return Job{}, ErrApplyBusy
	}
	r.active = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
	}()

	rev, err := r.revisions.Get()
	if err != nil {
		return Job{}, err
	}
	if revision != rev.Desired {
		return Job{}, fmt.Errorf("%w: requested=%d current=%d", ErrStaleRevision, revision, rev.Desired)
	}
	if revision < rev.Applied {
		return Job{}, fmt.Errorf("%w: requested=%d applied=%d", ErrRevisionRollback, revision, rev.Applied)
	}
	job := Job{
		ID:              uuid.NewString(),
		DesiredRevision: revision,
		BaseRevision:    rev.Applied,
		Status:          StatusPending,
		Trigger:         trigger,
		ActorID:         actor,
		CreatedAt:       nowUnix(),
	}
	if err := r.jobs.Create(job); err != nil {
		return Job{}, err
	}

	_ = r.jobs.MarkStatus(job.ID, StatusApplying, "", "")
	res, execErr := r.execute(revision)
	job.Operations = res.Operations
	_ = r.jobs.SetOperations(job.ID, res.Operations)

	if execErr == nil && res.Success {
		if err := r.revisions.MarkApplied(revision); err != nil {
			execErr = err
			res.ErrorCode = "REVISION_MARK_FAILED"
			res.ErrorMessage = err.Error()
		} else {
			_ = r.jobs.Finish(job.ID, StatusSucceeded, "", "")
			job.Status = StatusSucceeded
			return job, nil
		}
	}

	code := res.ErrorCode
	if code == "" {
		code = "APPLY_FAILED"
	}
	msg := res.ErrorMessage
	if msg == "" && execErr != nil {
		msg = execErr.Error()
	}
	status := StatusFailed
	if res.RolledBack {
		status = StatusRolledBack
	}
	_ = r.jobs.Finish(job.ID, status, code, msg)
	job.Status = status
	job.ErrorCode = code
	job.ErrorMessage = msg
	if execErr == nil {
		execErr = fmt.Errorf("apply revision %d failed: %s", revision, msg)
	}
	return job, execErr
}
