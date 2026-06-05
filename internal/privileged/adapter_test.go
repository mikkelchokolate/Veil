package privileged

import (
	"context"
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
	if _, err := adapter.StageUpdate(context.Background(), UpdateRequest{ArtifactID: "veil-linux-amd64"}); err != nil {
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
