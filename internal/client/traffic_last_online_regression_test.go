package client

import (
	"testing"
	"time"
)

// TestLastOnlineAtTracksCounterIncreases covers the presence "activity"
// signal stored on traffic_counters.last_online_at: it is written only when
// a sample's delta actually grew the counters — a first monotonic baseline
// and zero-delta re-observations must NOT mark activity, so presence never
// reports online for a merely-observed or never-used binding.
func TestLastOnlineAtTracksCounterIncreases(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "presence", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	key := "inst:" + b.ID

	lastOnline := func() (int64, bool) {
		t.Helper()
		var value *int64
		if err := db.QueryRow(`SELECT last_online_at FROM traffic_counters WHERE client_id=? AND binding_id=?`, c.ID, b.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if value == nil {
			return 0, false
		}
		return *value, true
	}

	// First monotonic observation establishes the baseline: delta 0 means no
	// activity — the counter row exists but last_online_at stays NULL.
	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 1000, DownloadBytes: 1000, AtUnix: 100, Monotonic: true, ProviderKey: key}); err != nil {
		t.Fatal(err)
	}
	if got, ok := lastOnline(); ok {
		t.Fatalf("baseline sample set last_online_at=%d, want NULL", got)
	}
	activity, err := ts.LastActivityByBinding([]string{c.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := activity[b.ID]; exists {
		t.Fatalf("LastActivityByBinding reported binding %s before any increase", b.ID)
	}

	// A growing counter marks activity at the sample's own timestamp.
	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 1500, DownloadBytes: 1000, AtUnix: 200, Monotonic: true, ProviderKey: key}); err != nil {
		t.Fatal(err)
	}
	if got, ok := lastOnline(); !ok || got != 200 {
		t.Fatalf("last_online_at=%v (ok=%v), want 200", got, ok)
	}

	// A zero-delta re-observation refreshes last_observed_at only — the
	// quiet binding does not look newly active.
	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 1500, DownloadBytes: 1000, AtUnix: 300, Monotonic: true, ProviderKey: key}); err != nil {
		t.Fatal(err)
	}
	if got, ok := lastOnline(); !ok || got != 200 {
		t.Fatalf("after zero-delta sample last_online_at=%v, want 200", got)
	}

	// The next increase moves the timestamp again.
	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 1600, DownloadBytes: 1000, AtUnix: 400, Monotonic: true, ProviderKey: key}); err != nil {
		t.Fatal(err)
	}
	if got, ok := lastOnline(); !ok || got != 400 {
		t.Fatalf("last_online_at=%v, want 400", got)
	}
}

// TestLastOnlineAtCreditsRestartGrowth: a post-restart reading is credited
// as a full delta (the counters provably grew since the reset), so it marks
// activity — the runtime really was talking. A restart that resets counters
// to exactly zero credits no delta and records no activity.
func TestLastOnlineAtCreditsRestartGrowth(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "restart-presence", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	key := "inst:" + b.ID

	record := func(up, down, at int64, instance string) {
		t.Helper()
		if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: up, DownloadBytes: down, AtUnix: at, Monotonic: true, ProviderKey: key, RuntimeInstanceID: instance}); err != nil {
			t.Fatalf("record(up=%d inst=%q): %v", up, instance, err)
		}
	}

	record(500, 500, 100, "pid-1") // baseline
	record(900, 900, 200, "pid-2") // restart credit: full reading is growth
	var lastOnline *int64
	if err := db.QueryRow(`SELECT last_online_at FROM traffic_counters WHERE client_id=? AND binding_id=?`, c.ID, b.ID).Scan(&lastOnline); err != nil {
		t.Fatal(err)
	}
	if lastOnline == nil || *lastOnline != 200 {
		t.Fatalf("restart-credit last_online_at=%v, want 200", lastOnline)
	}

	// Restart with a zero reading: nothing flowed since the reset, so no
	// activity is recorded and the timestamp is NOT refreshed.
	record(0, 0, 300, "pid-3")
	if err := db.QueryRow(`SELECT last_online_at FROM traffic_counters WHERE client_id=? AND binding_id=?`, c.ID, b.ID).Scan(&lastOnline); err != nil {
		t.Fatal(err)
	}
	if lastOnline == nil || *lastOnline != 200 {
		t.Fatalf("zero-reading restart last_online_at=%v, want 200 (unchanged)", lastOnline)
	}
}

// TestLastOnlineAtSurvivesQuotaRebuild: the periodic quota rollover rebuilds
// traffic_counters from retained bucket samples; the rebuild must preserve
// the last-increase timestamp or every reset would silently blank presence.
func TestLastOnlineAtSurvivesQuotaRebuild(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "rebuild-presence", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	key := "inst:" + b.ID

	base := time.Now().Unix() - 120
	for _, sm := range []Sample{
		{BindingID: b.ID, UploadBytes: 100, DownloadBytes: 0, AtUnix: base, Monotonic: true, ProviderKey: key},       // baseline
		{BindingID: b.ID, UploadBytes: 200, DownloadBytes: 0, AtUnix: base + 60, Monotonic: true, ProviderKey: key},  // +100
		{BindingID: b.ID, UploadBytes: 350, DownloadBytes: 0, AtUnix: base + 120, Monotonic: true, ProviderKey: key}, // +150
	} {
		if err := ts.RecordSample(sm); err != nil {
			t.Fatalf("record %+v: %v", sm, err)
		}
	}
	var before int64
	if err := db.QueryRow(`SELECT last_online_at FROM traffic_counters WHERE client_id=? AND binding_id=?`, c.ID, b.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != base+120 {
		t.Fatalf("pre-rebuild last_online_at=%d, want %d", before, base+120)
	}

	// Rebuild the current period starting just before the samples; the
	// rebuilt last_online_at is the newest positive-delta bucket.
	if err := ts.WithRecordLock(func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := ResetQuotaPeriodTx(tx, c.ID, base-60); err != nil {
			return err
		}
		return tx.Commit()
	}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	var rebuilt *int64
	if err := db.QueryRow(`SELECT last_online_at FROM traffic_counters WHERE client_id=? AND binding_id=?`, c.ID, b.ID).Scan(&rebuilt); err != nil {
		t.Fatal(err)
	}
	// Bucket timestamps truncate to the minute; the rebuild may only lose
	// sub-minute precision, never wipe the timestamp or move it forward.
	if rebuilt == nil {
		t.Fatal("rebuild wiped last_online_at — presence history lost across quota rollover")
	}
	if *rebuilt > before {
		t.Fatalf("rebuilt last_online_at=%d is NEWER than the last real increase %d", *rebuilt, before)
	}
	if *rebuilt < before-60 {
		t.Fatalf("rebuilt last_online_at=%d lost more than bucket truncation from %d", *rebuilt, before)
	}
}

// TestLastActivityByBindingScopesToRequestedClients: the presence fan-out
// query returns per-binding timestamps only for the clients asked about and
// only for bindings that have ever increased.
func TestLastActivityByBindingScopesToRequestedClients(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c1, _ := repo.Create(Client{Name: "one", Enabled: true, QuotaResetPolicy: ResetNever})
	c2, _ := repo.Create(Client{Name: "two", Enabled: true, QuotaResetPolicy: ResetNever})
	b1, _ := repo.CreateBinding(Binding{ClientID: c1.ID, InboundID: "in-1", Enabled: true})
	b2, _ := repo.CreateBinding(Binding{ClientID: c2.ID, InboundID: "in-1", Enabled: true})

	if err := ts.RecordSample(Sample{BindingID: b1.ID, UploadBytes: 10, AtUnix: 111}); err != nil {
		t.Fatal(err)
	}
	if err := ts.RecordSample(Sample{BindingID: b2.ID, UploadBytes: 20, AtUnix: 222}); err != nil {
		t.Fatal(err)
	}

	got, err := ts.LastActivityByBinding([]string{c1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[b1.ID] != 111 {
		t.Fatalf("LastActivityByBinding(%s)=%v, want {%s:111}", c1.ID, got, b1.ID)
	}
	if _, leaked := got[b2.ID]; leaked {
		t.Fatalf("LastActivityByBinding leaked other client's binding %s", b2.ID)
	}

	both, err := ts.LastActivityByBinding([]string{c1.ID, c2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 || both[b1.ID] != 111 || both[b2.ID] != 222 {
		t.Fatalf("LastActivityByBinding(both)=%v", both)
	}
}
