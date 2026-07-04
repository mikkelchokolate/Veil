package privileged

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAdapterValidatesBeforeCallingExecutor(t *testing.T) {
	called := false
	adapter := NewLocalAdapter(testPolicy(t), Executor{
		ServiceAction: func(_ context.Context, request ServiceActionRequest) error {
			called = true
			return nil
		},
	})

	err := adapter.ServiceAction(context.Background(), ServiceActionRequest{
		Unit:   "ssh.service",
		Action: ServiceActionRestart,
	})
	assertOperationErrorCode(t, err, ErrorForbiddenOperation)
	if called {
		t.Fatal("executor called for forbidden request")
	}
}

func TestAdapterCallsExecutorWithResolvedValues(t *testing.T) {
	var promotion ResolvedPromotion
	var journal ResolvedJournal
	var backup ResolvedBackup
	var update ResolvedUpdate
	adapter := NewLocalAdapter(testPolicy(t), Executor{
		Promote: func(_ context.Context, input ResolvedPromotion) (PromoteResult, error) {
			promotion = input
			return PromoteResult{WrittenArtifacts: []string{"mieru"}}, nil
		},
		Journal: func(_ context.Context, input ResolvedJournal) (JournalResult, error) {
			journal = input
			return JournalResult{Unit: input.Unit}, nil
		},
		Backup: func(_ context.Context, input ResolvedBackup) (BackupResult, error) {
			backup = input
			return BackupResult{ArchiveName: input.ArchiveName}, nil
		},
		Update: func(_ context.Context, input ResolvedUpdate) (UpdateResult, error) {
			update = input
			return UpdateResult{ArtifactID: input.ArtifactID, Staged: true}, nil
		},
	})

	if _, err := adapter.Promote(context.Background(), PromoteRequest{ArtifactIDs: []string{"mieru"}}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := adapter.Journal(context.Background(), JournalRequest{Unit: "veil.service", Lines: 5000}); err != nil {
		t.Fatalf("journal: %v", err)
	}
	if _, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupActionVerify, ArchiveName: "daily.enc"}); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if _, err := adapter.StageUpdate(context.Background(), UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "v0.6.0"}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if len(promotion.Artifacts) != 1 || promotion.Artifacts[0].ID != "mieru" {
		t.Fatalf("promotion not resolved: %+v", promotion)
	}
	if journal.Lines != 1000 {
		t.Fatalf("journal lines not clamped: %+v", journal)
	}
	if backup.ArchiveName != "daily.enc" {
		t.Fatalf("backup not resolved: %+v", backup)
	}
	if update.Path == "" || update.ArtifactID != "veil-linux-amd64" {
		t.Fatalf("update not resolved: %+v", update)
	}
}

func TestAdapterReturnsStableOperationFailedError(t *testing.T) {
	adapter := NewLocalAdapter(testPolicy(t), Executor{})
	_, err := adapter.ServiceStatus(context.Background(), ServiceStatusRequest{Units: []string{"veil.service"}})
	assertOperationErrorCode(t, err, ErrorOperationFailed)
}

func TestAdapterRotateKeyRestartPanelAndSyncCaddyCert(t *testing.T) {
	rotated := false
	restarted := false
	synced := false
	adapter := NewLocalAdapter(testPolicy(t), Executor{
		RotateKey: func(context.Context, RotateKeyRequest) error {
			rotated = true
			return nil
		},
		RestartPanel: func(context.Context) error {
			restarted = true
			return nil
		},
		SyncCaddyCert: func(context.Context, SyncCaddyCertRequest) (SyncCaddyCertResult, error) {
			synced = true
			return SyncCaddyCertResult{Found: true}, nil
		},
	})
	if err := adapter.RotateKey(context.Background(), RotateKeyRequest{}); err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	if err := adapter.RestartPanel(context.Background()); err != nil {
		t.Fatalf("restart panel: %v", err)
	}
	result, err := adapter.SyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com"})
	if err != nil {
		t.Fatalf("sync caddy cert: %v", err)
	}
	if !rotated || !restarted || !synced || !result.Found {
		t.Fatalf("expected all executors to be called: rotated=%t restarted=%t synced=%t found=%t", rotated, restarted, synced, result.Found)
	}
}

func TestAdapterFirewallApply(t *testing.T) {
	called := false
	adapter := NewLocalAdapter(testPolicy(t), Executor{
		Firewall: func(_ context.Context, resolved ResolvedFirewall) (FirewallResult, error) {
			called = true
			if len(resolved.RuleIDs) != 1 {
				t.Fatalf("unexpected resolved rules: %+v", resolved)
			}
			return FirewallResult{AppliedRuleIDs: resolved.RuleIDs}, nil
		},
	})
	result, err := adapter.FirewallApply(context.Background(), FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}})
	if err != nil {
		t.Fatalf("firewall apply: %v", err)
	}
	if !called || len(result.AppliedRuleIDs) != 1 {
		t.Fatalf("unexpected result: %+v called=%t", result, called)
	}
}

func TestAdapterReturnsOperationFailedWhenExecutorNil(t *testing.T) {
	adapter := NewLocalAdapter(testPolicy(t), Executor{})
	cases := []func() error{
		func() error {
			_, err := adapter.Promote(context.Background(), PromoteRequest{ArtifactIDs: []string{"mieru"}})
			return err
		},
		func() error {
			return adapter.ServiceAction(context.Background(), ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart})
		},
		func() error {
			_, err := adapter.ServiceStatus(context.Background(), ServiceStatusRequest{Units: []string{"veil.service"}})
			return err
		},
		func() error {
			_, err := adapter.Journal(context.Background(), JournalRequest{Unit: "veil.service"})
			return err
		},
		func() error {
			_, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupActionVerify, ArchiveName: "daily.enc"})
			return err
		},
		func() error { return adapter.RotateKey(context.Background(), RotateKeyRequest{}) },
		func() error {
			_, err := adapter.FirewallApply(context.Background(), FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}})
			return err
		},
		func() error {
			_, err := adapter.StageUpdate(context.Background(), UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "v0.6.0"})
			return err
		},
		func() error { return adapter.RestartPanel(context.Background()) },
		func() error {
			_, err := adapter.SyncCaddyCert(context.Background(), SyncCaddyCertRequest{Domain: "example.com"})
			return err
		},
	}
	for i, fn := range cases {
		if err := fn(); err == nil {
			t.Fatalf("case %d expected error", i)
		} else {
			assertOperationErrorCode(t, err, ErrorOperationFailed)
		}
	}
}

func TestAdapterPropagatesExecutorErrors(t *testing.T) {
	plain := errors.New("executor failed")
	adapter := NewLocalAdapter(testPolicy(t), Executor{
		Promote: func(context.Context, ResolvedPromotion) (PromoteResult, error) { return PromoteResult{}, plain },
		ServiceStatus: func(context.Context, ServiceStatusRequest) (ServiceStatusResult, error) {
			return ServiceStatusResult{}, plain
		},
		Journal:  func(context.Context, ResolvedJournal) (JournalResult, error) { return JournalResult{}, plain },
		Backup:   func(context.Context, ResolvedBackup) (BackupResult, error) { return BackupResult{}, plain },
		Update:   func(context.Context, ResolvedUpdate) (UpdateResult, error) { return UpdateResult{}, plain },
		Firewall: func(context.Context, ResolvedFirewall) (FirewallResult, error) { return FirewallResult{}, plain },
	})
	cases := []func() error{
		func() error {
			_, err := adapter.Promote(context.Background(), PromoteRequest{ArtifactIDs: []string{"mieru"}})
			return err
		},
		func() error {
			_, err := adapter.ServiceStatus(context.Background(), ServiceStatusRequest{Units: []string{"veil.service"}})
			return err
		},
		func() error {
			_, err := adapter.Journal(context.Background(), JournalRequest{Unit: "veil.service"})
			return err
		},
		func() error {
			_, err := adapter.Backup(context.Background(), BackupRequest{Action: BackupActionVerify, ArchiveName: "daily.enc"})
			return err
		},
		func() error {
			_, err := adapter.StageUpdate(context.Background(), UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "v0.6.0"})
			return err
		},
		func() error {
			_, err := adapter.FirewallApply(context.Background(), FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}})
			return err
		},
	}
	for i, fn := range cases {
		err := fn()
		if err == nil {
			t.Fatalf("case %d expected error", i)
		}
		assertOperationErrorCode(t, err, ErrorOperationFailed)
		if !strings.Contains(err.Error(), plain.Error()) {
			t.Fatalf("case %d expected wrapped message to contain %q, got %v", i, plain.Error(), err)
		}
	}
}
