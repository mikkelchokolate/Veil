package privileged

import "context"

type Client interface {
	Promote(context.Context, PromoteRequest) (PromoteResult, error)
	ServiceAction(context.Context, ServiceActionRequest) error
	ServiceStatus(context.Context, ServiceStatusRequest) (ServiceStatusResult, error)
	Journal(context.Context, JournalRequest) (JournalResult, error)
	Backup(context.Context, BackupRequest) (BackupResult, error)
	RotateKey(context.Context, RotateKeyRequest) error
	RecoverKeyRotation(context.Context, RecoverKeyRotationRequest) error
	FirewallApply(context.Context, FirewallRequest) (FirewallResult, error)
	StageUpdate(context.Context, UpdateRequest) (UpdateResult, error)
	RestartPanel(context.Context) error
	SyncCaddyCert(context.Context, SyncCaddyCertRequest) (SyncCaddyCertResult, error)
	// Reachable reports whether the privileged backend can still be reached.
	// A client object outlives its transport (a detached socket alias or a
	// stopped helper leaves a non-nil client), so health reporting must probe
	// reachability instead of trusting client presence. Implementations with
	// no separate transport return nil.
	Reachable(context.Context) error
}

type CaddyLoader interface {
	CaddyLoad(context.Context, CaddyLoadRequest) error
}

// BackupSftpOperator is the optional Client extension for SFTP remote-backup
// destination operations. The server discovers it by type assertion (the
// same pattern as CaddyLoader) so test doubles that only need the core
// Client surface stay compilable.
type BackupSftpOperator interface {
	BackupSftp(context.Context, BackupSftpRequest) (BackupSftpResult, error)
}
