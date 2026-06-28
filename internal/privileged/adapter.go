package privileged

import "context"

type LocalAdapter struct {
	policy   Policy
	executor Executor
}

func NewLocalAdapter(policy Policy, executor Executor) *LocalAdapter {
	return &LocalAdapter{policy: policy, executor: executor}
}

func (a *LocalAdapter) Promote(ctx context.Context, request PromoteRequest) (PromoteResult, error) {
	resolved, err := a.policy.ResolvePromotion(request)
	if err != nil {
		return PromoteResult{}, err
	}
	if a.executor.Promote == nil {
		return PromoteResult{}, newError(ErrorOperationFailed, "promote executor is unavailable")
	}
	result, err := a.executor.Promote(ctx, resolved)
	return result, wrapOperationError(err)
}

func (a *LocalAdapter) ServiceAction(ctx context.Context, request ServiceActionRequest) error {
	if err := a.policy.ValidateServiceAction(request); err != nil {
		return err
	}
	if a.executor.ServiceAction == nil {
		return newError(ErrorOperationFailed, "service action executor is unavailable")
	}
	return wrapOperationError(a.executor.ServiceAction(ctx, request))
}

func (a *LocalAdapter) ServiceStatus(ctx context.Context, request ServiceStatusRequest) (ServiceStatusResult, error) {
	if err := a.policy.ValidateServiceStatus(request); err != nil {
		return ServiceStatusResult{}, err
	}
	if a.executor.ServiceStatus == nil {
		return ServiceStatusResult{}, newError(ErrorOperationFailed, "service status executor is unavailable")
	}
	result, err := a.executor.ServiceStatus(ctx, request)
	return result, wrapOperationError(err)
}

func (a *LocalAdapter) Journal(ctx context.Context, request JournalRequest) (JournalResult, error) {
	resolved, err := a.policy.ResolveJournal(request)
	if err != nil {
		return JournalResult{}, err
	}
	if a.executor.Journal == nil {
		return JournalResult{}, newError(ErrorOperationFailed, "journal executor is unavailable")
	}
	result, err := a.executor.Journal(ctx, resolved)
	return result, wrapOperationError(err)
}

func (a *LocalAdapter) Backup(ctx context.Context, request BackupRequest) (BackupResult, error) {
	resolved, err := a.policy.ResolveBackup(request)
	if err != nil {
		return BackupResult{}, err
	}
	if a.executor.Backup == nil {
		return BackupResult{}, newError(ErrorOperationFailed, "backup executor is unavailable")
	}
	result, err := a.executor.Backup(ctx, resolved)
	return result, wrapOperationError(err)
}

func (a *LocalAdapter) RotateKey(ctx context.Context, request RotateKeyRequest) error {
	if a.executor.RotateKey == nil {
		return newError(ErrorOperationFailed, "key rotation executor is unavailable")
	}
	return wrapOperationError(a.executor.RotateKey(ctx, request))
}

func (a *LocalAdapter) FirewallApply(ctx context.Context, request FirewallRequest) (FirewallResult, error) {
	resolved, err := a.policy.ResolveFirewall(request)
	if err != nil {
		return FirewallResult{}, err
	}
	if a.executor.Firewall == nil {
		return FirewallResult{}, newError(ErrorOperationFailed, "firewall executor is unavailable")
	}
	result, err := a.executor.Firewall(ctx, resolved)
	return result, wrapOperationError(err)
}

func (a *LocalAdapter) StageUpdate(ctx context.Context, request UpdateRequest) (UpdateResult, error) {
	resolved, err := a.policy.ResolveUpdate(request)
	if err != nil {
		return UpdateResult{}, err
	}
	if a.executor.Update == nil {
		return UpdateResult{}, newError(ErrorOperationFailed, "update executor is unavailable")
	}
	result, err := a.executor.Update(ctx, resolved)
	return result, wrapOperationError(err)
}

func (a *LocalAdapter) RestartPanel(ctx context.Context) error {
	if a.executor.RestartPanel == nil {
		return newError(ErrorOperationFailed, "Panel restart executor is unavailable")
	}
	return wrapOperationError(a.executor.RestartPanel(ctx))
}

func (a *LocalAdapter) SyncCaddyCert(ctx context.Context, request SyncCaddyCertRequest) (SyncCaddyCertResult, error) {
	if a.executor.SyncCaddyCert == nil {
		return SyncCaddyCertResult{}, newError(ErrorOperationFailed, "sync caddy cert executor is unavailable")
	}
	result, err := a.executor.SyncCaddyCert(ctx, request)
	return result, wrapOperationError(err)
}

var _ Client = (*LocalAdapter)(nil)
