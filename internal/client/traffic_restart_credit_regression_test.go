package client

import (
	"testing"
	"time"
)

// TestMonotonicResetCreditsPostRestartUsage (#1102): a hysteria2/mieru
// restart resets the provider's cumulative counters to zero. The bytes
// accumulated since the reset are still visible in the next absolute
// reading, so that reading itself is the delta — the store must credit it
// instead of dropping it as a negative delta.
func TestMonotonicResetCreditsPostRestartUsage(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "restart", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	key := "inst-1:" + b.ID

	record := func(up, down, at int64) {
		t.Helper()
		if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: up, DownloadBytes: down, AtUnix: at, Monotonic: true, ProviderKey: key}); err != nil {
			t.Fatalf("record(up=%d): %v", up, err)
		}
	}

	record(1000, 2000, 1)
	record(5000, 9000, 2) // uptime accrual: +4000/+7000
	// Runtime restarts: counter resets to zero and climbs to 40/70 before
	// the next poll. Those 40/70 bytes happened after the restart and must
	// be credited.
	record(40, 70, 3)
	record(60, 90, 4)

	up, down, err := ts.TotalsForClient(c.ID)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	// Pre-restart delta (+4000/+7000), restart credit (40/70), then +20/+20.
	if up != 4060 || down != 7090 {
		t.Fatalf("totals = up %d down %d, want 4060/7090", up, down)
	}
}

// TestMonotonicFirstObservationCreditsNothing covers the bootstrap case:
// the very first reading establishes the baseline and credits nothing, so
// a fresh runtime is not charged for bytes it reported before polling
// began.
func TestMonotonicFirstObservationCreditsNothing(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "baseline", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})

	if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: 5000, DownloadBytes: 5000, AtUnix: 1, Monotonic: true, ProviderKey: "i:" + b.ID}); err != nil {
		t.Fatalf("record: %v", err)
	}
	up, down, err := ts.TotalsForClient(c.ID)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	if up != 0 || down != 0 {
		t.Fatalf("first observation credited %d/%d, want 0/0 baseline", up, down)
	}
}

// TestPruneSamplesRetention (#1138): bucketed samples previously grew
// without bound. Pruning removes only rows older than the retention window
// and the store enforces a floor so a pending monthly quota rebuild always
// keeps enough history.
func TestPruneSamplesRetention(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "prune", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})

	now := time.Unix(2_000_000_000, 0)
	old := now.Add(-120 * 24 * time.Hour).Unix()  // beyond 90d retention
	recent := now.Add(-24 * time.Hour).Unix()     // inside retention
	floor := now.Add(-40 * 24 * time.Hour).Unix() // inside min floor window
	for _, ts0 := range []int64{old, recent, floor} {
		if _, err := db.Exec(`INSERT INTO traffic_samples (bucket_start, client_id, binding_id, upload_delta, download_delta)
		  VALUES (?,?,?,1,1)`, ts0, c.ID, b.ID); err != nil {
			t.Fatalf("seed sample %d: %v", ts0, err)
		}
	}

	removed, err := ts.PruneSamples(DefaultSampleRetention, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("pruned %d rows, want 1", removed)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM traffic_samples`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("remaining samples=%d, want 2", count)
	}

	// Retention floor: asking for a 1h window clamps to minSampleRetention —
	// the recent row survives, and no quota rebuild window is violated.
	if _, err := db.Exec(`INSERT INTO traffic_samples (bucket_start, client_id, binding_id, upload_delta, download_delta)
	  VALUES (?,?,?,1,1)`, now.Add(-50*24*time.Hour).Unix(), c.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	removed, err = ts.PruneSamples(time.Hour, now)
	if err != nil {
		t.Fatalf("floor prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("floor prune removed %d, want 1 (only the 50d row)", removed)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM traffic_samples`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("after floor prune remaining=%d, want 2 (recent + 40d row kept)", count)
	}
}

// TestRuntimeInstanceChangeCreditsFullReading (#1102): when the process
// instance id changes the daemon restarted and its cumulative counters reset
// — even when the new absolute reading is already ABOVE the pre-restart
// baseline, which the negative-delta heuristic alone cannot detect. The
// whole reading is post-restart usage and must be credited in full.
func TestRuntimeInstanceChangeCreditsFullReading(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "instance", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	key := "inst:" + b.ID

	record := func(up, down, at int64, instance string) {
		t.Helper()
		if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: up, DownloadBytes: down, AtUnix: at, Monotonic: true, ProviderKey: key, RuntimeInstanceID: instance}); err != nil {
			t.Fatalf("record(up=%d inst=%q): %v", up, instance, err)
		}
	}

	record(0, 0, 1, "pid-100")
	record(500, 500, 2, "pid-100") // +500/+500 uptime accrual
	// Daemon restarts; before the next poll the new process already counted
	// 900MB — above the old baseline of 500. Without the instance id this
	// reads as +400 and silently drops 500MB of real usage.
	record(900, 900, 3, "pid-200")
	record(950, 950, 4, "pid-200") // +50/+50 normal delta on the new instance

	up, down, err := ts.TotalsForClient(c.ID)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	// 500 (pre-restart) + 900 (full post-reset reading) + 50 = 1450 each way.
	if up != 1450 || down != 1450 {
		t.Fatalf("totals = up %d down %d, want 1450/1450", up, down)
	}
}

// TestRuntimeInstanceSameKeepsDeltaPath (#1102): an unchanged instance id
// must not alter the normal delta computation, and an empty incoming id
// preserves the stored instance rather than wiping it.
func TestRuntimeInstanceSameKeepsDeltaPath(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "stable", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})
	key := "inst:" + b.ID

	record := func(up, down, at int64, instance string) {
		t.Helper()
		if err := ts.RecordSample(Sample{BindingID: b.ID, UploadBytes: up, DownloadBytes: down, AtUnix: at, Monotonic: true, ProviderKey: key, RuntimeInstanceID: instance}); err != nil {
			t.Fatalf("record(up=%d inst=%q): %v", up, instance, err)
		}
	}

	record(100, 100, 1, "pid-1")
	record(150, 150, 2, "pid-1")
	record(200, 200, 3, "") // id-less sample keeps instance pid-1
	record(250, 250, 4, "pid-1")

	up, down, err := ts.TotalsForClient(c.ID)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	if up != 150 || down != 150 {
		t.Fatalf("totals = up %d down %d, want 150/150", up, down)
	}
	var storedInstance string
	if err := db.QueryRow(`SELECT runtime_instance FROM traffic_runtime_state WHERE provider_key=?`, key).Scan(&storedInstance); err != nil {
		t.Fatal(err)
	}
	if storedInstance != "pid-1" {
		t.Fatalf("stored instance %q, want pid-1 preserved", storedInstance)
	}
}
