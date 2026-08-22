package client

import (
	"errors"
	"testing"
	"time"
)

func TestQuotaRolloverAdvancesToFirstFutureUTCBoundaryAndRetainsHistory(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 27, 15, 4, 5, 0, time.UTC)
	tests := []struct {
		name     string
		policy   string
		resetAt  time.Time
		wantNext time.Time
	}{
		{
			name:     "daily after seven missed periods",
			policy:   ResetDaily,
			resetAt:  time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC),
			wantNext: time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "weekly after multiple missed periods",
			policy:   ResetWeekly,
			resetAt:  time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
			wantNext: time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "monthly across year boundary",
			policy:   ResetMonthly,
			resetAt:  time.Date(2025, time.November, 1, 0, 0, 0, 0, time.UTC),
			wantNext: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t)
			defer db.Close()
			repo := NewRepository(db)
			traffic := NewTrafficStore(db)
			quota := int64(100)
			resetAt := tc.resetAt.Unix()
			c, err := repo.Create(Client{
				Name: "period-client", Enabled: true, QuotaBytes: &quota,
				QuotaResetPolicy: tc.policy, QuotaResetAt: &resetAt, Depleted: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			binding, err := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 90, DownloadBytes: 20, AtUnix: fixedNow.Add(-time.Hour).Unix()}); err != nil {
				t.Fatal(err)
			}

			reconciler := NewReconciler(repo, traffic, 0, nil)
			reconciler.now = func() time.Time { return fixedNow }
			changed, err := reconciler.ReconcileOnce()
			if err != nil {
				t.Fatalf("rollover: %v", err)
			}
			if changed != 1 {
				t.Errorf("changed=%d want=1", changed)
			}
			got, err := repo.Get(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.QuotaResetAt == nil || *got.QuotaResetAt != tc.wantNext.Unix() {
				t.Errorf("quotaResetAt=%v want=%d (%s)", got.QuotaResetAt, tc.wantNext.Unix(), tc.wantNext)
			}
			if got.Depleted {
				t.Error("depleted must clear in the same rollover commit")
			}
			up, down, err := traffic.TotalsForClient(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if up != 0 || down != 0 {
				t.Errorf("current-period usage=%d/%d want=0/0", up, down)
			}
			history, err := traffic.HistoryForClient(c.ID, 0, fixedNow.Unix(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(history) != 1 || history[0].UploadDelta != 90 || history[0].DownloadDelta != 20 {
				t.Errorf("lifetime analytics were deleted or changed: %+v", history)
			}

			// A new-period sample must survive the next reconcile. This catches an
			// already-expired resetAt that causes rollover on every pass.
			if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 7, DownloadBytes: 3, AtUnix: fixedNow.Unix()}); err != nil {
				t.Fatal(err)
			}
			if _, err := reconciler.ReconcileOnce(); err != nil {
				t.Fatalf("second reconcile: %v", err)
			}
			up, down, _ = traffic.TotalsForClient(c.ID)
			if up != 7 || down != 3 {
				t.Errorf("new-period usage was reset again: got=%d/%d want=7/3", up, down)
			}
		})
	}
}

func TestQuotaRolloverPropagatesStorageErrorWithoutPartialMutation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(100)
	resetAt := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC).Unix()
	clientRow, err := repo.Create(Client{Name: "atomic", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetMonthly, QuotaResetAt: &resetAt, Depleted: true})
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := repo.CreateBinding(Binding{ClientID: clientRow.ID, InboundID: "in-1", Enabled: true})
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 75, DownloadBytes: 50, AtUnix: resetAt - 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER quota_reset_fault BEFORE DELETE ON traffic_counters BEGIN SELECT RAISE(ABORT, 'injected quota reset fault'); END`); err != nil {
		t.Fatal(err)
	}

	callbackCalls := 0
	reconciler := NewReconciler(repo, traffic, 0, func(string, bool) error {
		callbackCalls++
		return nil
	})
	reconciler.now = func() time.Time { return time.Date(2026, time.July, 27, 15, 0, 0, 0, time.UTC) }
	_, err = reconciler.ReconcileOnce()
	if err == nil {
		t.Fatal("injected reset storage error was ignored")
	}
	if callbackCalls != 0 {
		t.Fatalf("state mutation callback ran after failed reset: %d", callbackCalls)
	}
	got, _ := repo.Get(clientRow.ID)
	if !got.Depleted || got.QuotaResetAt == nil || *got.QuotaResetAt != resetAt {
		t.Fatalf("client partially mutated after reset error: %+v", got)
	}
	up, down, totalsErr := traffic.TotalsForClient(clientRow.ID)
	if totalsErr != nil {
		t.Fatal(totalsErr)
	}
	if up != 75 || down != 50 {
		t.Fatalf("usage partially changed after reset error: %d/%d", up, down)
	}
}

func TestQuotaRolloverCallbackFailureRollsBackUsageAndClientState(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(100)
	resetAt := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC).Unix()
	clientRow, _ := repo.Create(Client{Name: "callback-atomic", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetMonthly, QuotaResetAt: &resetAt, Depleted: true})
	binding, _ := repo.CreateBinding(Binding{ClientID: clientRow.ID, InboundID: "in-1", Enabled: true})
	_ = traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 80, DownloadBytes: 30, AtUnix: resetAt - 60})

	injected := errors.New("injected revision/snapshot/apply coordination failure")
	reconciler := NewReconciler(repo, traffic, 0, func(string, bool) error { return injected })
	reconciler.now = func() time.Time { return time.Date(2026, time.July, 27, 15, 0, 0, 0, time.UTC) }
	_, err := reconciler.ReconcileOnce()
	if !errors.Is(err, injected) {
		t.Fatalf("error=%v want injected callback failure", err)
	}
	up, down, _ := traffic.TotalsForClient(clientRow.ID)
	if up != 80 || down != 30 {
		t.Fatalf("usage reset escaped failed transaction: %d/%d", up, down)
	}
	got, _ := repo.Get(clientRow.ID)
	if !got.Depleted || got.QuotaResetAt == nil || *got.QuotaResetAt != resetAt {
		t.Fatalf("client state changed despite rollback: %+v", got)
	}
}

func TestFixedIntervalQuotaPolicyIsRejectedUntilCanonicalIntervalExists(t *testing.T) {
	quota := int64(100)
	err := validate(Client{Name: "no-ambiguous-period", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: "fixed_interval"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("fixed_interval accepted without canonical interval/anchor: %v", err)
	}
}
