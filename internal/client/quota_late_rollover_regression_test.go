package client

import (
	"testing"
	"time"
)

func TestLateQuotaRolloverKeepsTrafficRecordedAfterBoundary(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	quota := int64(10000)
	boundary := time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC)
	resetAt := boundary.Unix()
	created, err := repo.Create(Client{
		Name: "late-rollover", Enabled: true, QuotaBytes: &quota,
		QuotaResetPolicy: ResetDaily, QuotaResetAt: &resetAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: created.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{
		BindingID: binding.ID, UploadBytes: 500, DownloadBytes: 50, AtUnix: resetAt - 60,
	}); err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordSample(Sample{
		BindingID: binding.ID, UploadBytes: 1000, DownloadBytes: 25, AtUnix: resetAt + 30,
	}); err != nil {
		t.Fatal(err)
	}

	reconciler := NewReconciler(repo, traffic, 0, nil)
	reconciler.now = func() time.Time { return boundary.Add(time.Minute) }
	if _, err := reconciler.ReconcileOnce(); err != nil {
		t.Fatalf("delayed rollover: %v", err)
	}
	up, down, err := traffic.TotalsForClient(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 1000 || down != 25 {
		t.Fatalf("current-period totals=%d/%d, want 1000/25 recorded after the boundary", up, down)
	}
	history, err := traffic.HistoryForClient(created.ID, 0, boundary.Add(time.Hour).Unix(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("analytics history rows=%d, want both old and new period samples", len(history))
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC).Unix()
	if got.QuotaResetAt == nil || *got.QuotaResetAt != wantNext {
		t.Fatalf("quotaResetAt=%v, want %d", got.QuotaResetAt, wantNext)
	}
}
