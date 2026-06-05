package privileged

import "context"

type Executor struct {
	Promote       func(context.Context, ResolvedPromotion) (PromoteResult, error)
	ServiceAction func(context.Context, ServiceActionRequest) error
	ServiceStatus func(context.Context, ServiceStatusRequest) (ServiceStatusResult, error)
	Journal       func(context.Context, ResolvedJournal) (JournalResult, error)
	Backup        func(context.Context, ResolvedBackup) (BackupResult, error)
	RotateKey     func(context.Context, RotateKeyRequest) error
	Firewall      func(context.Context, ResolvedFirewall) (FirewallResult, error)
	Update        func(context.Context, ResolvedUpdate) (UpdateResult, error)
	RestartPanel  func(context.Context) error
}
