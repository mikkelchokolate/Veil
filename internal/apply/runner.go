package apply

import (
	"context"
	"database/sql"
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

const (
	PublicationPhaseIntent              = "intent"
	PublicationPhaseArtifactsPrepared   = "artifacts_prepared"
	PublicationPhaseArtifactsCommitted  = "artifacts_committed"
	PublicationPhaseServicesPlanned     = "services_planned"
	PublicationPhaseServicesConverged   = "services_converged"
	PublicationPhaseHealthVerified      = "health_verified"
	PublicationPhaseFirewallCommitted   = "firewall_committed"
	PublicationPhaseSideEffectPlanned   = "side_effect_planned"
	PublicationPhaseSideEffectCommitted = "side_effect_committed"
	PublicationPhaseSideEffectVerified  = "side_effect_verified"
	PublicationPhasePublished           = "published"
	PublicationPhaseFinalized           = "finalized"
	PublicationPhaseRolledBack          = "rolled_back"
	PublicationPhaseRecoveryTransferred = "recovery_transferred"
)

type PublicationDetails struct {
	ExpectedLiveManifestSHA256 string            `json:"expectedLiveManifestSHA256,omitempty"`
	PreviousLiveManifestSHA256 string            `json:"previousLiveManifestSHA256,omitempty"`
	Artifacts                  []string          `json:"artifacts,omitempty"`
	LiveRoot                   string            `json:"liveRoot,omitempty"`
	ServicePhase               string            `json:"servicePhase,omitempty"`
	FirewallPhase              string            `json:"firewallPhase,omitempty"`
	ServiceActionPlan          []string          `json:"serviceActionPlan,omitempty"`
	PreviousServiceStates      map[string]string `json:"previousServiceStates,omitempty"`
	ExpectedServiceGeneration  string            `json:"expectedServiceGeneration,omitempty"`
	ExpectedConfigDigest       string            `json:"expectedConfigDigest,omitempty"`
	FirewallTransactionID      string            `json:"firewallTransactionId,omitempty"`
	PreviousFirewallDigest     string            `json:"previousFirewallDigest,omitempty"`
	IntendedFirewallDigest     string            `json:"intendedFirewallDigest,omitempty"`
	HealthEvidence             []string          `json:"healthEvidence,omitempty"`
	UpdateTransactionID        string            `json:"updateTransactionId,omitempty"`
	ExpectedBinaryDigest       string            `json:"expectedBinaryDigest,omitempty"`
	OldBinaryDigest            string            `json:"oldBinaryDigest,omitempty"`
	InstalledInode             string            `json:"installedInode,omitempty"`
	TargetVersion              string            `json:"targetVersion,omitempty"`
	ActivationManifest         string            `json:"activationManifest,omitempty"`
	CommitPhase                string            `json:"commitPhase,omitempty"`
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
func MarkSideEffectStarting(ctx context.Context, details PublicationDetails) error {
	return AdvanceRuntimePublication(ctx, PublicationPhaseSideEffectPlanned, details)
}

func MarkRuntimeMutationStarting(ctx context.Context, details PublicationDetails) error {
	return AdvanceRuntimePublication(ctx, PublicationPhaseArtifactsPrepared, details)
}

func AdvanceRuntimePublication(ctx context.Context, phase string, details PublicationDetails) error {
	if ctx == nil {
		return nil
	}
	if record, ok := ctx.Value(publicationContextKey{}).(func(string, PublicationDetails) error); ok {
		return record(phase, details)
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
	Success          bool
	Disposition      ApplyDisposition
	MarkRevisionLive bool
	RuntimeMutation  RuntimeMutationOutcome
	RolledBack       bool
	ErrorCode        string
	ErrorMessage     string
	Operations       []OperationResult
	Confirmations    []EnforcementConfirmation
}

type confirmationExecutor struct {
	underlying    Executor
	confirmations []EnforcementConfirmation
}

func (e confirmationExecutor) Execute(revision uint64) (Result, error) {
	return e.ExecuteContext(context.Background(), revision)
}

func (e confirmationExecutor) ExecuteContext(ctx context.Context, revision uint64) (Result, error) {
	var result Result
	var err error
	if contextual, ok := e.underlying.(ContextExecutor); ok {
		result, err = contextual.ExecuteContext(ctx, revision)
	} else {
		result, err = e.underlying.Execute(revision)
	}
	result.Confirmations = append(result.Confirmations, e.confirmations...)
	return result, err
}

func (e confirmationExecutor) RequiresDurablePhases() bool {
	_, contextual := e.underlying.(ContextExecutor)
	return contextual
}

type executorPhasePolicy interface {
	RequiresDurablePhases() bool
}

type ApplyDisposition string

const (
	ApplyDispositionPlanned            ApplyDisposition = "planned"
	ApplyDispositionStaged             ApplyDisposition = "staged"
	ApplyDispositionArtifactsCommitted ApplyDisposition = "artifacts_committed"
	ApplyDispositionRuntimeConverged   ApplyDisposition = "runtime_converged"
)

type RuntimeMutationOutcome struct {
	MutationStarted        bool
	ArtifactsChanged       bool
	ServicesChanged        bool
	FirewallChanged        bool
	ArtifactsRestored      bool
	ServicesRestored       bool
	FirewallRestored       bool
	PostRollbackHealthPass bool
	RollbackComplete       bool
	Ambiguous              bool
}

type EnforcementConfirmation struct {
	Kind              string `json:"kind"`
	ClientID          string `json:"clientId"`
	TargetGeneration  int64  `json:"targetGeneration"`
	TargetPayloadHash string `json:"targetPayloadHash"`
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
	if err := runner.resumeRecoveryPending(context.Background()); err != nil {
		runner.startupErr = err
		go runner.monitorRecovery()
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
			if lease.Owner == r.ownerID && lease.ExpiresAt > now.Unix() {
				due, dueErr := r.jobs.RecoveryPendingDue(now.Add(-5 * time.Second).Unix())
				if dueErr != nil {
					r.setRecoveryError(dueErr)
					continue
				}
				if !due {
					continue
				}
				r.setRecoveryError(nil)
				r.setRecoveryError(r.resumeRecoveryPending(context.Background()))
				continue
			}
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
				r.setRecoveryError(nil)
				recoveryErr = r.resumeRecoveryPending(context.Background())
			}
			if recoveryErr == nil {
				recoveryErr = r.jobs.MarkApplyingInterrupted("apply job had no valid durable lease during continuous recovery")
			}
			r.setRecoveryError(recoveryErr)
		}
	}
}

func (r *Runner) resumeRecoveryPending(ctx context.Context) error {
	if r == nil || r.revs == nil || r.revs.db == nil {
		return nil
	}
	var job Job
	var servicePhase string
	err := r.revs.db.QueryRowContext(ctx, `SELECT j.id,j.desired_revision,j.base_revision,j.trigger,j.actor_id,j.owner_process,j.lease_generation,p.service_phase
FROM apply_jobs j JOIN runtime_publications p ON p.job_id=j.id
WHERE j.status=? ORDER BY j.created_at,j.id LIMIT 1`, StatusRecoveryPending).Scan(
		&job.ID, &job.DesiredRevision, &job.BaseRevision, &job.Trigger, &job.ActorID,
		&job.OwnerProcess, &job.LeaseGeneration, &servicePhase)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("apply: read recovery-pending publication: %w", err)
	}
	revs, err := r.revs.Get()
	if err != nil {
		return fmt.Errorf("apply: read revisions during recovery: %w", err)
	}
	if job.DesiredRevision <= revs.Applied {
		// A later job already marked the panel applied. Re-running this
		// revision would fail finalize (`applied_revision<=desired` is false)
		// and pin startupErr, blocking every subsequent apply.
		if err := r.jobs.Finish(job.ID, StatusFailed, "SUPERSEDED",
			fmt.Sprintf("recovery-pending revision %d is already covered by applied revision %d", job.DesiredRevision, revs.Applied)); err != nil {
			return fmt.Errorf("apply: close superseded recovery job: %w", err)
		}
		return r.resumeRecoveryPending(ctx)
	}
	if servicePhase == "restart-panel" || servicePhase == "update-install" {
		return fmt.Errorf("apply: side-effect publication %s requires helper-owned commit evidence", job.ID)
	}
	if job.OwnerProcess != r.ownerID || job.LeaseGeneration == 0 {
		return fmt.Errorf("%w: recovery-pending publication %s is not fenced to this runner", ErrApplyBusy, job.ID)
	}
	if err := advanceRuntimePublicationPhase(r.revs.db, job.ID, job.LeaseGeneration, PublicationPhaseRecoveryTransferred, PublicationDetails{}, r.now().UTC().Unix()); err != nil {
		return fmt.Errorf("apply: transfer recovery evidence: %w", err)
	}
	if err := finalizeFencedJob(r.revs.db, r.ownerID, job.LeaseGeneration, r.now(), job, StatusFailed,
		"PUBLICATION_RECOVERY_TRANSFERRED", "runtime publication evidence transferred to a fresh full-convergence attempt", nil, nil, false, false); err != nil {
		return fmt.Errorf("apply: finalize transferred recovery job: %w", err)
	}
	_, err = r.runContext(ctx, job.DesiredRevision, "publication-recovery", "system", r.executor)
	if err != nil {
		return fmt.Errorf("apply: resume full convergence for revision %d: %w", job.DesiredRevision, err)
	}
	return r.resumeRecoveryPending(ctx)
}

func (r *Runner) setRecoveryError(err error) {
	r.mu.Lock()
	r.startupErr = err
	r.mu.Unlock()
}

func (r *Runner) ReadinessError() error {
	if r == nil {
		return errors.New("apply: runner is unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startupErr
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
	executor := confirmationExecutor{underlying: r.executor, confirmations: confirmations}
	return r.runContext(ctx, revision, trigger, actor, executor)
}

// RunLatest applies the current desired revision and keeps applying as long as
// a newer desired revision is published while a job is in flight. Concurrent
// callers wait for the active job instead of returning ErrApplyBusy and leaving
// the system pending forever.
func (r *Runner) RunLatest(ctx context.Context, trigger, actor string) (Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var last Job
	for {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		revs, err := r.revs.Get()
		if err != nil {
			return last, err
		}
		if revs.Desired <= revs.Applied {
			return last, nil
		}
		attempted := revs.Desired
		job, err := r.RunContext(ctx, attempted, trigger, actor)
		if job.ID != "" {
			last = job
		}
		if errors.Is(err, ErrApplyBusy) {
			if waitErr := r.waitIdle(ctx); waitErr != nil {
				return last, waitErr
			}
			continue
		}
		if errors.Is(err, ErrStaleRevision) {
			continue
		}
		if err != nil {
			return last, err
		}
		after, afterErr := r.revs.Get()
		if afterErr != nil {
			return last, afterErr
		}
		if after.Applied < attempted {
			return last, nil
		}
	}
}

func (r *Runner) waitIdle(ctx context.Context) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		idle := !r.active
		r.mu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
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
	execCtx = context.WithValue(execCtx, publicationContextKey{}, func(phase string, details PublicationDetails) error {
		return advanceRuntimePublicationPhase(r.revs.db, job.ID, lease.Generation, phase, details, r.now().UTC().Unix())
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
		safeRollback := !result.RuntimeMutation.MutationStarted ||
			(result.RuntimeMutation.RollbackComplete && !result.RuntimeMutation.Ambiguous)
		if !safeRollback {
			pendingErr := markFinalizationPending(r.revs.db, r.ownerID, lease.Generation, r.now(), job.ID, execErr)
			if pendingErr == nil {
				leaseReleased = true
			}
			job.Status = StatusRecoveryPending
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

	if result.Disposition == "" {
		return job, errors.New("apply: successful executor result has no disposition")
	}
	if result.MarkRevisionLive != (result.Disposition == ApplyDispositionRuntimeConverged) {
		return job, errors.New("apply: revision-live marker does not match runtime convergence disposition")
	}
	if result.Disposition == ApplyDispositionStaged {
		if err := finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusStaged, "", "", result.Operations, nil, false, false); err != nil {
			return job, err
		}
		leaseReleased = true
		job.Status = StatusStaged
		job.Operations = result.Operations
		return job, nil
	}

	_, strictPhases := executor.(ContextExecutor)
	if policy, ok := executor.(executorPhasePolicy); ok {
		strictPhases = policy.RequiresDurablePhases()
	}
	if err := recordRuntimePublication(r.revs.db, job, lease.Generation, result.Disposition, result.Operations, result.Confirmations, !strictPhases, r.now().UTC().Unix()); err != nil {
		pendingErr := markFinalizationPending(r.revs.db, r.ownerID, lease.Generation, r.now(), job.ID, err)
		if pendingErr == nil {
			leaseReleased = true
		}
		job.Status = StatusRecoveryPending
		job.ErrorCode = "PUBLICATION_RECEIPT_PENDING"
		job.ErrorMessage = err.Error()
		return job, errors.Join(err, pendingErr)
	}
	if err := finalizeFencedJob(r.revs.db, r.ownerID, lease.Generation, r.now(), job, StatusSucceeded, "", "", result.Operations, result.Confirmations, result.MarkRevisionLive, false); err != nil {
		pendingErr := markFinalizationPending(r.revs.db, r.ownerID, lease.Generation, r.now(), job.ID, err)
		if pendingErr == nil {
			leaseReleased = true
		}
		job.Status = StatusRecoveryPending
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
