package client

import (
	"testing"
	"time"
)

func TestLateQuotaRolloverKeepsUsageRecordedAfterBoundary(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(10_000)
	boundary := time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC)
	resetAt := boundary.Unix()
	created, err := repo.Create(Client{
		Name: "late-rollover", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetDaily, QuotaResetAt: &resetAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "in-1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 1000, DownloadBytes: 0, AtUnix: boundary.Add(30 * time.Second).Unix()}); err != nil {
		t.Fatal(err)
	}

	reconciler := NewReconciler(repo, traffic, 0, nil)
	reconciler.now = func() time.Time { return boundary.Add(60 * time.Second) }
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatal(err)
	}
	up, down, err := traffic.TotalsForClient(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 1000 || down != 0 {
		t.Fatalf("late rollover dropped new-period usage: %d/%d", up, down)
	}
	history, err := traffic.HistoryForClient(created.ID, 0, boundary.Add(time.Hour).Unix(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].UploadDelta != 1000 {
		t.Fatalf("history changed by rollover: %+v", history)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC).Unix()
	if got.QuotaResetAt == nil || *got.QuotaResetAt != wantNext {
		t.Fatalf("quotaResetAt=%v want %d", got.QuotaResetAt, wantNext)
	}
}
