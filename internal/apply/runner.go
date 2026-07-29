package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
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
	Owner      string
	Generation uint64
}

type fenceContextKey struct{}

func ContextWithFence(ctx context.Context, fence Fence) context.Context {
	return context.WithValue(ctx, fenceContextKey{}, fence)
}

func FenceFromContext(ctx context.Context) (Fence, bool) {
	if ctx == nil {
		return Fence{}, false
	}
	fence, ok := ctx.Value(fenceContextKey{}).(Fence)
	return fence, ok && fence.Owner != "" && fence.Generation > 0
}

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
	monitor, err := runner.recoverStartup()
	if err != nil {
		runner.startupErr = err
		close(runner.monitorDone)
		return runner
	}
	if monitor {
		go runner.monitorForeignLease()
	} else {
		close(runner.monitorDone)
	}
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

func (r *Runner) monitorForeignLease() {
	defer close(r.monitorDone)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.monitorStop:
			return
		case <-ticker.C:
			lease, err := r.leases.Current()
			if err != nil {
				r.mu.Lock()
				r.startupErr = err
				r.mu.Unlock()
				return
			}
			if lease.Owner == "" || lease.Owner == r.ownerID {
				return
			}
			if lease.ExpiresAt > r.now().Unix() && processOwnerAlive(lease.Owner) {
				continue
			}
			if err := r.leases.Expire(lease.Owner, lease.Generation); err != nil && !errors.Is(err, ErrApplyLeaseLost) {
				r.mu.Lock()
				r.startupErr = err
				r.mu.Unlock()
				return
			}
			if err := recoverRuntimePublications(r.revs.db, r.leases, r.jobs, r.ownerID, r.now, r.leaseTTL); err == nil {
				err = r.jobs.MarkApplyingInterrupted("apply owner lease expired after panel startup")
			}
			if err != nil {
				r.mu.Lock()
				r.startupErr = err
				r.mu.Unlock()
			}
			return
		}
	}
}

func (r *Runner) Close() {
	r.closeOnce.Do(func() { close(r.monitorStop) })
	<-r.monitorDone
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

	operation := trigger + ":revision:" + fmt.Sprint(revision)
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
		ID: uuid.NewString(), DesiredRevision: revision, BaseRevision: revisions.Applied,
		Status: StatusPending, Trigger: trigger, ActorID: actor, CreatedAt: nowUnix(),
		OwnerProcess: r.ownerID, LeaseGeneration: lease.Generation,
	}
	if err := r.jobs.Create(job); err != nil {
		return job, err
	}
	if err := r.jobs.MarkStatus(job.ID, StatusApplying, "", ""); err != nil {
		_ = finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusFailed, "job_store", err.Error(), nil, false, false)
		return job, err
	}
	job.Status = StatusApplying

	execCtx, cancelExecutor := context.WithCancel(ContextWithFence(ctx, Fence{Owner: r.ownerID, Generation: lease.Generation}))
	stopHeartbeat := make(chan struct{})
	heartbeatErr := make(chan error, 1)
	go r.heartbeat(execCtx, lease.Generation, stopHeartbeat, heartbeatErr, cancelExecutor)
	var result Result
	var execErr error
	if executor, ok := r.executor.(ContextExecutor); ok {
		result, execErr = executor.ExecuteContext(execCtx, revision)
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
	cancelExecutor()

	if execErr != nil {
		code := result.ErrorCode
		if code == "" {
			code = "apply_failed"
		}
		finishErr := finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusFailed, code, execErr.Error(), result.Operations, false, false)
		if finishErr == nil {
			leaseReleased = true
		}
		job.Status = StatusFailed
		job.ErrorCode = code
		job.ErrorMessage = execErr.Error()
		job.Operations = result.Operations
		return job, errors.Join(execErr, finishErr)
	}

	if err := recordRuntimePublication(r.revs.db, job, lease.Generation, result.Operations, r.now().UTC().Unix()); err != nil {
		finishErr := finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusFailed, "PUBLICATION_RECEIPT_FAILED", err.Error(), result.Operations, false, false)
		if finishErr == nil {
			leaseReleased = true
		}
		return job, errors.Join(err, finishErr)
	}
	if err := finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusSucceeded, "", "", result.Operations, true, false); err != nil {
		pendingErr := markFinalizationPending(r.revs.db, r.ownerID, lease.Generation, r.now(), job.ID, err)
		if pendingErr == nil {
			leaseReleased = true
		}
		job.Status = StatusFailed
		job.ErrorCode = "FINALIZATION_PENDING"
		job.ErrorMessage = err.Error()
		return job, errors.Join(err, pendingErr)
	}
	leaseReleased = true
	job.Status = StatusSucceeded
	job.Operations = result.Operations
	return job, nil
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
			if err := r.leases.Heartbeat(r.ownerID, generation, r.now(), r.leaseTTL); err != nil {
				cancel()
				result <- fmt.Errorf("apply: heartbeat durable lease: %w", err)
				return
			}
		}
	}
}
