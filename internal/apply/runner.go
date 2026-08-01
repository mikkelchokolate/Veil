package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
)

var (
	ErrApplyBusy       = errors.New("apply: another apply job is active")
	ErrStaleRevision   = errors.New("apply: requested revision is not the current desired revision")
	ErrRevisionApplied = errors.New("apply: requested revision is older than the applied revision")
)

type Fence struct {
	Owner          string
	Generation     uint64
	OperationID    string
	LeaseExpiresAt int64
}

type fenceContextKey struct{}
type publicationContextKey struct{}

type PublicationDetails struct {
	ExpectedLiveManifestSHA256 string
	PreviousLiveManifestSHA256 string
	Artifacts                  []string
	ServicePhase               string
	FirewallPhase              string
	LiveRoot                   string
}

type fenceContextState struct {
	owner       string
	generation  uint64
	operationID string
	expiresAt   atomic.Int64
}

func ContextWithFence(ctx context.Context, fence Fence) context.Context {
	state := &fenceContextState{owner: fence.Owner, generation: fence.Generation, operationID: fence.OperationID}
	state.expiresAt.Store(fence.LeaseExpiresAt)
	return context.WithValue(ctx, fenceContextKey{}, state)
}

func FenceFromContext(ctx context.Context) (Fence, bool) {
	if ctx == nil {
		return Fence{}, false
	}
	state, ok := ctx.Value(fenceContextKey{}).(*fenceContextState)
	if !ok || state.owner == "" || state.generation == 0 {
		return Fence{}, false
	}
	return Fence{Owner: state.owner, Generation: state.generation, OperationID: state.operationID,
		LeaseExpiresAt: state.expiresAt.Load()}, true
}

func updateFenceExpiry(ctx context.Context, expiresAt int64) {
	if state, ok := ctx.Value(fenceContextKey{}).(*fenceContextState); ok {
		state.expiresAt.Store(expiresAt)
	}
}

// MarkRuntimeMutationStarting durably advances the publication transaction
// before the first live side effect. It is a no-op for untracked test/dev
// workflows that have no Runner-owned publication context.
func MarkRuntimeMutationStarting(ctx context.Context, details PublicationDetails) error {
	if ctx == nil {
		return nil
	}
	if record, ok := ctx.Value(publicationContextKey{}).(func(PublicationDetails) error); ok {
		return record(details)
	}
	return nil
}

type Executor interface {
	Execute(revision uint64) (Result, error)
}

type ContextExecutor interface {
	Executor
	ExecuteContext(context.Context, uint64) (Result, error)
}

type Result struct {
	Success       bool
	RolledBack    bool
	ErrorCode     string
	ErrorMessage  string
	Operations    []OperationResult
	Confirmations []EnforcementConfirmation
}

type EnforcementConfirmation struct {
	Kind     string `json:"kind"`
	ClientID string `json:"clientId"`
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

	monitorStop chan struct{}
	monitorDone chan struct{}
	closeOnce   sync.Once
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
		ownerID:  fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString()),
		leaseTTL: 30 * time.Second, heartbeatInterval: 10 * time.Second, now: time.Now,
		monitorStop: make(chan struct{}), monitorDone: make(chan struct{}),
	}
	if revs == nil || revs.db == nil || jobs == nil {
		runner.startupErr = errors.New("apply: runner stores are not configured")
		close(runner.monitorDone)
		return runner
	}
	runner.leases = NewLeaseStore(revs.db)
	_, err := runner.recoverStartup()
	if err != nil {
		runner.startupErr = err
		close(runner.monitorDone)
		return runner
	}
	go runner.monitorRecovery()
	return runner
}

func (r *Runner) recoverStartup() (bool, error) {
	lease, err := r.leases.Current()
	if err != nil {
		return false, fmt.Errorf("apply: inspect startup lease: %w", err)
	}
	now := r.now()
	if lease.Owner != "" && lease.ExpiresAt > now.Unix() && processOwnerAlive(lease.Owner) {
		return true, nil
	}
	if lease.Owner != "" {
		if err := r.leases.Expire(lease.Owner, lease.Generation); err != nil && !errors.Is(err, ErrApplyLeaseLost) {
			return false, err
		}
	}
	if err := recoverRuntimePublications(r.revs.db, r.leases, r.jobs, r.ownerID, r.now, r.leaseTTL); err != nil {
		return false, err
	}
	if err := r.jobs.MarkApplyingInterrupted("panel process restarted before apply completed"); err != nil {
		return false, err
	}
	return false, nil
}

func processOwnerAlive(owner string) bool {
	if !strings.HasPrefix(owner, "pid:") {
		return true
	}
	parts := strings.SplitN(owner, ":", 3)
	if len(parts) < 2 {
		return false
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (r *Runner) monitorRecovery() {
	defer close(r.monitorDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.monitorStop:
			return
		case <-ticker.C:
			r.mu.Lock()
			active := r.active
			r.mu.Unlock()
			if active {
				continue
			}
			lease, err := r.leases.Current()
			if err != nil {
				r.setRecoveryError(err)
				continue
			}
			now := r.now()
			if lease.Owner != "" && lease.ExpiresAt > now.Unix() && processOwnerAlive(lease.Owner) {
				continue
			}
			if lease.Owner != "" {
				if err := r.leases.Expire(lease.Owner, lease.Generation); err != nil && !errors.Is(err, ErrApplyLeaseLost) {
					r.setRecoveryError(err)
					continue
				}
			}
			recoveryErr := recoverRuntimePublications(r.revs.db, r.leases, r.jobs, r.ownerID, r.now, r.leaseTTL)
			if recoveryErr == nil {
				recoveryErr = r.jobs.MarkApplyingInterrupted("apply job had no valid durable lease during continuous recovery")
			}
			r.setRecoveryError(recoveryErr)
		}
	}
}

func (r *Runner) setRecoveryError(err error) {
	r.mu.Lock()
	r.startupErr = err
	r.mu.Unlock()
}

func (r *Runner) Close() {
	r.closeOnce.Do(func() { close(r.monitorStop) })
	<-r.monitorDone
}

func (r *Runner) Run(revision uint64, trigger, actor string) (job Job, runErr error) {
	return r.RunContext(context.Background(), revision, trigger, actor)
}

func (r *Runner) RunContext(ctx context.Context, revision uint64, trigger, actor string) (job Job, runErr error) {
	return r.runContext(ctx, revision, trigger, actor, r.executor)
}

// RunOperationContext routes a non-plan runtime mutation (for example an
// explicit service action) through the exact same durable lease, fencing,
// publication, job and finalization machinery as a normal revision apply.
func (r *Runner) RunOperationContext(ctx context.Context, revision uint64, trigger, actor string, executor Executor) (job Job, runErr error) {
	if executor == nil {
		return Job{}, errors.New("apply: operation executor is nil")
	}
	return r.runContext(ctx, revision, trigger, actor, executor)
}

func (r *Runner) RunContextWithConfirmations(ctx context.Context, revision uint64, trigger, actor string, confirmations ...EnforcementConfirmation) (Job, error) {
	executor := ContextExecutorFunc(func(operationContext context.Context, pinnedRevision uint64) (Result, error) {
		var result Result
		var err error
		if contextual, ok := r.executor.(ContextExecutor); ok {
			result, err = contextual.ExecuteContext(operationContext, pinnedRevision)
		} else {
			result, err = r.executor.Execute(pinnedRevision)
		}
		result.Confirmations = append(result.Confirmations, confirmations...)
		return result, err
	})
	return r.runContext(ctx, revision, trigger, actor, executor)
}

func (r *Runner) runContext(ctx context.Context, revision uint64, trigger, actor string, executor Executor) (job Job, runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return Job{}, ErrApplyBusy
	}
	if r.startupErr != nil {
		err := r.startupErr
		r.mu.Unlock()
		return Job{}, err
	}
	r.active = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
	}()
	if err := r.recoverBeforeAcquisition(); err != nil {
		return Job{}, err
	}

	jobID := uuid.NewString()
	operation := jobID
	lease, acquired, err := r.leases.Acquire(r.ownerID, operation, r.now(), r.leaseTTL)
	if err != nil {
		return Job{}, fmt.Errorf("apply: acquire durable lease: %w", err)
	}
	if !acquired {
		return Job{}, ErrApplyBusy
	}
	leaseReleased := false
	defer func() {
		if leaseReleased {
			return
		}
		if err := r.leases.Release(r.ownerID, lease.Generation); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("apply: release durable lease: %w", err))
		}
	}()

	revisions, err := r.revs.Get()
	if err != nil {
		return Job{}, err
	}
	if revision != revisions.Desired {
		return Job{}, fmt.Errorf("%w: requested=%d current=%d", ErrStaleRevision, revision, revisions.Desired)
	}
	if revision < revisions.Applied {
		return Job{}, fmt.Errorf("%w: requested=%d applied=%d", ErrRevisionApplied, revision, revisions.Applied)
	}

	job = Job{
		ID: jobID, DesiredRevision: revision, BaseRevision: revisions.Applied,
		Status: StatusPending, Trigger: trigger, ActorID: actor, CreatedAt: nowUnix(),
		OwnerProcess: r.ownerID, LeaseGeneration: lease.Generation,
	}
	if err := r.jobs.Create(job); err != nil {
		return job, err
	}
	if err := r.jobs.MarkStatus(job.ID, StatusApplying, "", ""); err != nil {
		_ = finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusFailed, "job_store", err.Error(), nil, nil, false, false)
		return job, err
	}
	job.Status = StatusApplying
	if err := recordRuntimePublicationIntent(r.revs.db, job, lease, r.now().UTC().Unix()); err != nil {
		finishErr := finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusFailed,
			"PUBLICATION_INTENT_FAILED", err.Error(), nil, nil, false, false)
		if finishErr == nil {
			leaseReleased = true
		}
		return job, errors.Join(err, finishErr)
	}

	execCtx := ContextWithFence(ctx, Fence{
		Owner: r.ownerID, Generation: lease.Generation, OperationID: lease.Operation, LeaseExpiresAt: lease.ExpiresAt,
	})
	execCtx = context.WithValue(execCtx, publicationContextKey{}, func(details PublicationDetails) error {
		return markRuntimePublicationPublishing(r.revs.db, job.ID, lease.Generation, details, r.now().UTC().Unix())
	})
	execCtx, cancelExecutor := context.WithCancel(execCtx)
	stopHeartbeat := make(chan struct{})
	heartbeatErr := make(chan error, 1)
	go r.heartbeat(execCtx, lease.Generation, stopHeartbeat, heartbeatErr, cancelExecutor)
	var result Result
	var execErr error
	if contextExecutor, ok := executor.(ContextExecutor); ok {
		result, execErr = contextExecutor.ExecuteContext(execCtx, revision)
	} else {
		result, execErr = executor.Execute(revision)
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
	cancelExecutor()

	if execErr != nil {
		code := result.ErrorCode
		if code == "" {
			code = "apply_failed"
		}
		safeRollback := result.RolledBack || len(result.Operations) == 0
		if !safeRollback {
			pendingErr := markFinalizationPending(r.revs.db, r.ownerID, lease.Generation, r.now(), job.ID, execErr)
			if pendingErr == nil {
				leaseReleased = true
			}
			job.Status = StatusApplying
			job.ErrorCode = "RECOVERY_PENDING"
			job.ErrorMessage = execErr.Error()
			job.Operations = result.Operations
			return job, errors.Join(execErr, pendingErr)
		}
		rollbackErr := markRuntimePublicationRolledBack(r.revs.db, job.ID, lease.Generation, r.now().UTC().Unix())
		var finishErr error
		if rollbackErr == nil {
			finishErr = finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusFailed, code, execErr.Error(), result.Operations, nil, false, false)
		}
		finishErr = errors.Join(rollbackErr, finishErr)
		if finishErr == nil {
			leaseReleased = true
		}
		job.Status = StatusFailed
		job.ErrorCode = code
		job.ErrorMessage = execErr.Error()
		job.Operations = result.Operations
		return job, errors.Join(execErr, finishErr)
	}

	if err := recordRuntimePublication(r.revs.db, job, lease.Generation, result.Operations, result.Confirmations, r.now().UTC().Unix()); err != nil {
		pendingErr := markFinalizationPending(r.revs.db, r.ownerID, lease.Generation, r.now(), job.ID, err)
		if pendingErr == nil {
			leaseReleased = true
		}
		job.Status = StatusApplying
		job.ErrorCode = "PUBLICATION_RECEIPT_PENDING"
		job.ErrorMessage = err.Error()
		return job, errors.Join(err, pendingErr)
	}
	if err := finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusSucceeded, "", "", result.Operations, result.Confirmations, true, false); err != nil {
		pendingErr := markFinalizationPending(r.revs.db, r.ownerID, lease.Generation, r.now(), job.ID, err)
		if pendingErr == nil {
			leaseReleased = true
		}
		job.Status = StatusApplying
		job.ErrorCode = "FINALIZATION_PENDING"
		job.ErrorMessage = err.Error()
		return job, errors.Join(err, pendingErr)
	}
	leaseReleased = true
	job.Status = StatusSucceeded
	job.Operations = result.Operations
	return job, nil
}

func (r *Runner) recoverBeforeAcquisition() error {
	valid, err := r.leases.Valid(r.now())
	if err != nil {
		return fmt.Errorf("apply: inspect lease before acquisition: %w", err)
	}
	if valid {
		return nil
	}
	if err := recoverRuntimePublications(r.revs.db, r.leases, r.jobs, r.ownerID, r.now, r.leaseTTL); err != nil {
		return err
	}
	return r.jobs.MarkApplyingInterrupted("apply job had no valid durable lease before acquisition")
}

func (r *Runner) heartbeat(ctx context.Context, generation uint64, stop <-chan struct{}, result chan<- error, cancel context.CancelFunc) {
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
			now := r.now()
			if err := r.leases.Heartbeat(r.ownerID, generation, now, r.leaseTTL); err != nil {
				cancel()
				result <- fmt.Errorf("apply: heartbeat durable lease: %w", err)
				return
			}
			updateFenceExpiry(ctx, now.Add(r.leaseTTL).UTC().Unix())
		}
	}
}
