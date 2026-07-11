package privileged

import (
	"context"
	"strings"
	"testing"
)

func TestBackupAdapterRejectsRetentionOutsideAllowedRanges(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request BackupRequest
		want    string
	}{
		{name: "negative daily", request: BackupRequest{Action: BackupActionPrune, Daily: -1}, want: "daily retention"},
		{name: "excessive daily", request: BackupRequest{Action: BackupActionPrune, Daily: 366}, want: "daily retention"},
		{name: "negative weekly", request: BackupRequest{Action: BackupActionPrune, Weekly: -1}, want: "weekly retention"},
		{name: "excessive weekly", request: BackupRequest{Action: BackupActionPrune, Weekly: 105}, want: "weekly retention"},
		{name: "negative monthly", request: BackupRequest{Action: BackupActionPrune, Monthly: -1}, want: "monthly retention"},
		{name: "excessive monthly", request: BackupRequest{Action: BackupActionPrune, Monthly: 121}, want: "monthly retention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			adapter := NewLocalAdapter(testPolicy(t), Executor{
				Backup: func(context.Context, ResolvedBackup) (BackupResult, error) {
					called = true
					return BackupResult{}, nil
				},
			})
			_, err := adapter.Backup(context.Background(), tc.request)
			assertOperationErrorCode(t, err, ErrorInvalidRequest)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want containing %q", err, tc.want)
			}
			if called {
				t.Fatal("backup executor was called for invalid retention")
			}
		})
	}
}

func TestBackupAdapterAllowsRetentionBoundaries(t *testing.T) {
	called := false
	adapter := NewLocalAdapter(testPolicy(t), Executor{
		Backup: func(_ context.Context, resolved ResolvedBackup) (BackupResult, error) {
			called = true
			if resolved.Daily != 365 || resolved.Weekly != 104 || resolved.Monthly != 120 {
				t.Fatalf("resolved retention = %+v", resolved)
			}
			return BackupResult{}, nil
		},
	})
	_, err := adapter.Backup(context.Background(), BackupRequest{
		Action: BackupActionPrune,
		Daily: 365, Weekly: 104, Monthly: 120,
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if !called {
		t.Fatal("backup executor was not called for valid boundary values")
	}
}
