package privileged

import "context"

type Client interface {
	Promote(context.Context, PromoteRequest) (PromoteResult, error)
	ServiceAction(context.Context, ServiceActionRequest) error
	ServiceStatus(context.Context, ServiceStatusRequest) (ServiceStatusResult, error)
	Journal(context.Context, JournalRequest) (JournalResult, error)
	Backup(context.Context, BackupRequest) (BackupResult, error)
	RotateKey(context.Context, RotateKeyRequest) error
	FirewallApply(context.Context, FirewallRequest) (FirewallResult, error)
	StageUpdate(context.Context, UpdateRequest) (UpdateResult, error)
	RestartPanel(context.Context) error
}
