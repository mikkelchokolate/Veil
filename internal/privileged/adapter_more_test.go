package privileged

import (
	"context"
	"errors"
	"testing"
)

func TestAdapterPolicyValidationErrors(t *testing.T) {
	policy := testPolicy(t)
	adapter := NewLocalAdapter(policy, Executor{
		Promote: func(context.Context, ResolvedPromotion) (PromoteResult, error) {
			return PromoteResult{}, nil
		},
		ServiceStatus: func(context.Context, ServiceStatusRequest) (ServiceStatusResult, error) {
			return ServiceStatusResult{}, nil
		},
		Journal: func(context.Context, ResolvedJournal) (JournalResult, error) {
			return JournalResult{}, nil
		},
		Backup: func(context.Context, ResolvedBackup) (BackupResult, error) {
			return BackupResult{}, nil
		},
		Firewall: func(context.Context, ResolvedFirewall) (FirewallResult, error) {
			return FirewallResult{}, nil
		},
		Update: func(context.Context, ResolvedUpdate) (UpdateResult, error) {
			return UpdateResult{}, nil
		},
	})

	cases := []struct {
		name string
		fn   func() error
		code ErrorCode
	}{
		{
			name: "promote unknown artifact",
			fn: func() error {
				_, err := adapter.Promote(context.Background(), PromoteRequest{ArtifactIDs: []string{"unknown"}})
				return err
			},
			code: ErrorNotFound,
		},
		{
			name: "service status unmanaged unit",
			fn: func() error {
				_, err := adapter.ServiceStatus(context.Background(), ServiceStatusRequest{Units: []string{"ssh.service"}})
				return err
			},
			code: ErrorForbiddenOperation,
		},
		{
			name: "journal unmanaged unit",
			fn: func() error {
				_, err := adapter.Journal(context.Background(), JournalRequest{Unit: "ssh.service"})
				return err
			},
			code: ErrorForbiddenOperation,
		},
		{
			name: "backup unsupported action",
			fn: func() error {
				_, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupAction("purge")})
				return err
			},
			code: ErrorInvalidRequest,
		},
		{
			name: "firewall unknown rule",
			fn: func() error {
				_, err := adapter.FirewallApply(context.Background(), FirewallRequest{RuleIDs: []string{"unknown"}})
				return err
			},
			code: ErrorForbiddenOperation,
		},
		{
			name: "update unknown artifact",
			fn: func() error {
				_, err := adapter.StageUpdate(context.Background(), UpdateRequest{ArtifactID: "unknown", Version: "v1.0.0"})
				return err
			},
			code: ErrorNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertOperationErrorCode(t, tc.fn(), tc.code)
		})
	}
}

func TestAdapterServiceStatusExecutorError(t *testing.T) {
	plain := errors.New("status failed")
	adapter := NewLocalAdapter(testPolicy(t), Executor{
		ServiceStatus: func(context.Context, ServiceStatusRequest) (ServiceStatusResult, error) {
			return ServiceStatusResult{}, plain
		},
	})
	_, err := adapter.ServiceStatus(context.Background(), ServiceStatusRequest{Units: []string{"veil.service"}})
	assertOperationErrorCode(t, err, ErrorOperationFailed)
}
