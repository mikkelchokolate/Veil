package client

import (
	"testing"
	"time"
)

func TestReconcilerMarksDepletedAtQuota(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	quota := int64(1000)
	c, _ := repo.Create(Client{Name: "q", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	_ = ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 600, DownloadBytes: 500, AtUnix: 1})

	var flipped []string
	// The callback OWNS the depleted-flag write (blocker A2: it routes through
	// the unified mutation orchestration in production, which persists the
	// flag atomically with the revision bump + snapshot).
	rec := NewReconciler(repo, ts, 0, func(id string, depleted bool) error {
		if depleted {
			flipped = append(flipped, id)
		}
		return repo.SetDepleted(id, depleted)
	})
	changed, err := rec.ReconcileOnce()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	got, _ := repo.Get(c.ID)
	if !got.Depleted {
		t.Fatalf("client should be marked depleted")
	}
	if len(flipped) != 1 || flipped[0] != c.ID {
		t.Fatalf("onChange not fired for %s: %v", c.ID, flipped)
	}
}

func TestReconcilerNoQuotaNeverDepleted(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)
	c, _ := repo.Create(Client{Name: "u", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	_ = ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 1 << 40, DownloadBytes: 1 << 40, AtUnix: 1})
	rec := NewReconciler(repo, ts, 0, nil)
	changed, _ := rec.ReconcileOnce()
	if changed != 0 {
		t.Fatalf("changed = %d, want 0 (no quota)", changed)
	}
}

func TestReconcilerResetWindowRollover(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	quota := int64(1000)
	past := time.Now().Unix() - 3600 // reset window already passed
	c, _ := repo.Create(Client{Name: "r", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetMonthly, QuotaResetAt: &past, Depleted: true})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	_ = ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 5000, DownloadBytes: 5000, AtUnix: 1})

	rec := NewReconciler(repo, ts, 0, nil)
	changed, err := rec.ReconcileOnce()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1 (reset clears depleted)", changed)
	}
	got, _ := repo.Get(c.ID)
	if got.Depleted {
		t.Fatalf("depleted should be cleared on reset rollover")
	}
	up, down, _ := ts.TotalsForClient(c.ID)
	if up != 0 || down != 0 {
		t.Fatalf("counters should reset to 0 after rollover, got up=%d down=%d", up, down)
	}
}

func TestReconcilerClearsDepletedWhenQuotaRaised(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	quota := int64(1000)
	c, _ := repo.Create(Client{Name: "raise", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	_ = ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 2000, DownloadBytes: 0, AtUnix: 1})

	rec := NewReconciler(repo, ts, 0, nil)
	_, _ = rec.ReconcileOnce() // marks depleted
	got, _ := repo.Get(c.ID)
	if !got.Depleted {
		t.Fatalf("precondition: should be depleted")
	}
	// Admin raises the quota above usage -> reconciler clears the flag.
	bigger := int64(1 << 40)
	got.QuotaBytes = &bigger
	got.Depleted = true
	if _, err := repo.Update(got, got.Version); err != nil {
		t.Fatalf("update: %v", err)
	}
	changed, _ := rec.ReconcileOnce()
	if changed != 1 {
		t.Fatalf("changed = %d, want 1 (depleted cleared)", changed)
	}
	got2, _ := repo.Get(c.ID)
	if got2.Depleted {
		t.Fatalf("depleted should clear when quota raised above usage")
	}
}
