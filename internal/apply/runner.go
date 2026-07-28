package apply

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrApplyBusy       = errors.New("apply: another apply job is active")
	ErrStaleRevision   = errors.New("apply: requested revision is not the current desired revision")
	ErrRevisionApplied = errors.New("apply: requested revision is older than the applied revision")
)

type Executor interface {
	Execute(revision uint64) (Result, error)
}

type ContextExecutor interface {
	Executor
	ExecuteContext(context.Context, uint64) (Result, error)
}

type Result struct {
	Success      bool
	RolledBack   bool
	ErrorCode    string
	ErrorMessage string
	Operations   []OperationResult
}

type ExecutorFunc func(revision uint64) (Result, error)

func (f ExecutorFunc) Execute(revision uint64) (Result, error) { return f(revision) }

type ContextExecutorFunc func(context.Context, uint64) (Result, error)

func (f ContextExecutorFunc) Execute(revision uint64) (Result, error) {
	return f(context.Background(), revision)
}
func (f ContextExecutorFunc) ExecuteContext(ctx context.Context, revision uint64) (Result, error) {
	return f(ctx, revision)
}

type Runner struct {
	mu       sync.Mutex
	active   bool
	revs     *RevisionStore
	jobs     *JobStore
	leases   *LeaseStore
	executor Executor
	ownerID  string

	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	now               func() time.Time
	startupErr        error
}

func NewRunner(revs *RevisionStore, jobs *JobStore, executor any) *Runner {
	var resolved Executor
	switch value := executor.(type) {
	case ContextExecutor:
		resolved = value
	case Executor:
		resolved = value
	case func(uint64) (Result, error):
		resolved = ExecutorFunc(value)
	default:
		resolved = ExecutorFunc(func(uint64) (Result, error) {
			return Result{}, errors.New("apply: executor is not configured")
		})
	}
	runner := &Runner{
		revs: revs, jobs: jobs, executor: resolved,
		ownerID: uuid.NewString(), leaseTTL: 30 * time.Second,
		heartbeatInterval: 10 * time.Second, now: time.Now,
	}
	if revs == nil || revs.db == nil || jobs == nil {
		runner.startupErr = errors.New("apply: runner stores are not configured")
		return runner
	}
	runner.leases = NewLeaseStore(revs.db)
	valid, err := runner.leases.Valid(runner.now())
	if err != nil {
		runner.startupErr = fmt.Errorf("apply: inspect startup lease: %w", err)
	} else if !valid {
		runner.startupErr = jobs.MarkApplyingInterrupted("panel process restarted before apply completed")
	}
	return runner
}

func (r *Runner) Run(revision uint64, trigger, actor string) (job Job, runErr error) {
	return r.RunContext(context.Background(), revision, trigger, actor)
}

func (r *Runner) RunContext(ctx context.Context, revision uint64, trigger, actor string) (job Job, runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

	if r.startupErr != nil {
		return Job{}, r.startupErr
	}
	operation := trigger + ":revision:" + fmt.Sprint(revision)
	acquired, err := r.leases.Acquire(r.ownerID, operation, r.now(), r.leaseTTL)
	if err != nil {
		return Job{}, fmt.Errorf("apply: acquire durable lease: %w", err)
	}
	if !acquired {
		return Job{}, ErrApplyBusy
	}
	defer func() {
		if err := r.leases.Release(r.ownerID); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("apply: release durable lease: %w", err))
		}
	}()

	revs, err := r.revs.Get()
	if err != nil {
		return Job{}, err
	}
	if revision != revs.Desired {
		return Job{}, fmt.Errorf("%w: requested=%d current=%d", ErrStaleRevision, revision, revs.Desired)
	}
	if revision < revs.Applied {
		return Job{}, fmt.Errorf("%w: requested=%d applied=%d", ErrRevisionApplied, revision, revs.Applied)
	}

	job = Job{
		ID: uuid.NewString(), DesiredRevision: revision, BaseRevision: revs.Applied,
		Status: StatusPending, Trigger: trigger, ActorID: actor, CreatedAt: nowUnix(),
	}
	if err := r.jobs.Create(job); err != nil {
		return job, err
	}
	if err := r.jobs.MarkStatus(job.ID, StatusApplying, "", ""); err != nil {
		_ = r.jobs.Finish(job.ID, StatusFailed, "job_store", err.Error())
		return job, err
	}
	job.Status = StatusApplying

	stopHeartbeat := make(chan struct{})
	heartbeatErr := make(chan error, 1)
	go r.heartbeat(ctx, stopHeartbeat, heartbeatErr)
	var result Result
	var execErr error
	if executor, ok := r.executor.(ContextExecutor); ok {
		result, execErr = executor.ExecuteContext(ctx, revision)
	} else {
		result, execErr = r.executor.Execute(revision)
	}
	if execErr == nil && !result.Success {
		message := result.ErrorMessage
		if message == "" {
			message = "apply executor reported an unsuccessful result"
		}
		execErr = errors.New(message)
	}
	close(stopHeartbeat)
	if err := <-heartbeatErr; err != nil {
		execErr = errors.Join(execErr, err)
	}

	ops := result.Operations
	if err := r.jobs.SetOperations(job.ID, ops); err != nil {
		execErr = errors.Join(execErr, err)
	}
	if execErr != nil {
		code := result.ErrorCode
		if code == "" {
			code = "apply_failed"
		}
		finishErr := r.jobs.Finish(job.ID, StatusFailed, code, execErr.Error())
		job.Status = StatusFailed
		job.ErrorCode = code
		job.ErrorMessage = execErr.Error()
		job.Operations = ops
		return job, errors.Join(execErr, finishErr)
	}
	if err := r.revs.MarkApplied(revision); err != nil {
		_ = r.jobs.Finish(job.ID, StatusFailed, "revision_update_failed", err.Error())
		job.Status = StatusFailed
		return job, err
	}
	if err := r.jobs.Finish(job.ID, StatusSucceeded, "", ""); err != nil {
		job.Status = StatusApplying
		return job, err
	}
	job.Status = StatusSucceeded
	job.Operations = ops
	return job, nil
}

func (r *Runner) heartbeat(ctx context.Context, stop <-chan struct{}, result chan<- error) {
	ticker := time.NewTicker(r.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			result <- ctx.Err()
			return
		case <-stop:
			result <- nil
			return
		case <-ticker.C:
			if err := r.leases.Heartbeat(r.ownerID, r.now(), r.leaseTTL); err != nil {
				result <- fmt.Errorf("apply: heartbeat durable lease: %w", err)
				return
			}
		}
	}
}
