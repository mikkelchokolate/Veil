package client

import (
	"testing"
)

// TestTrafficSampleAttribution verifies a sample is attributed to the correct
// client/binding and accumulates byte counters.
func TestTrafficSampleAttribution(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-hy2", Enabled: true})

	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 100, DownloadBytes: 250, AtUnix: 1000}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 50, DownloadBytes: 50, AtUnix: 1010}); err != nil {
		t.Fatalf("record2: %v", err)
	}

	up, down, err := ts.TotalsForClient(c.ID)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	if up != 150 || down != 300 {
		t.Fatalf("totals = up %d down %d, want 150/300", up, down)
	}
}

// TestTrafficMonotonicPerSample verifies the store accepts monotonic byte
// counters and the reconciler can diff successive provider readings without
// double-counting.
func TestTrafficMonotonicPerSample(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "m", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})

	// Two readings of a monotonic provider counter.
	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 1000, DownloadBytes: 0, AtUnix: 1, Monotonic: true, ProviderKey: "p1"}); err != nil {
		t.Fatalf("record1: %v", err)
	}
	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 1500, DownloadBytes: 0, AtUnix: 2, Monotonic: true, ProviderKey: "p1"}); err != nil {
		t.Fatalf("record2: %v", err)
	}

	// The cumulative counter must reflect only the diff (500), not the sum.
	up, _, err := ts.TotalsForClient(c.ID)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	if up != 500 {
		t.Fatalf("monotonic cumulative up = %d, want 500 (delta of 1500-1000)", up)
	}
	// The raw runtime state holds the last absolute reading (1500).
	rawUp, _, err := ts.MonotonicTotals(map[string][]string{"p1": {b.ID}})
	if err != nil {
		t.Fatalf("monotonic totals: %v", err)
	}
	if rawUp != 1500 {
		t.Fatalf("monotonic raw total up = %d, want last reading 1500", rawUp)
	}
}

// TestTrafficHistoryQuery verifies a bounded history window is queryable.
func TestTrafficHistoryQuery(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "h", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	// Spread samples across distinct minute buckets.
	for i := int64(0); i < 5; i++ {
		_ = ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: i * 10, DownloadBytes: i, AtUnix: (100 + i) * 60})
	}
	samples, err := ts.HistoryForBinding(b.ID, 0, 100000, 100)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(samples) != 5 {
		t.Fatalf("history len = %d, want 5", len(samples))
	}
	// Window filter across two adjacent minute buckets.
	samples2, _ := ts.HistoryForBinding(b.ID, 102*60, 103*60, 100)
	if len(samples2) != 2 {
		t.Fatalf("windowed history len = %d, want 2", len(samples2))
	}
}

// TestTrafficQuotaEnforcement verifies quota/exhaustion is derivable.
func TestTrafficQuotaEnforcement(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	quota := int64(1000)
	c, _ := repo.Create(Client{Name: "q", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	_ = ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 600, DownloadBytes: 500, AtUnix: 1})

	_, down, _ := ts.TotalsForClient(c.ID)
	used := int64(0)
	up, _, _ := ts.TotalsForClient(c.ID)
	used = up + down
	if used != 1100 {
		t.Fatalf("used = %d", used)
	}
	exhausted := c.QuotaBytes != nil && used >= *c.QuotaBytes
	if !exhausted {
		t.Fatalf("expected quota exhausted (used %d >= %d)", used, *c.QuotaBytes)
	}
}
