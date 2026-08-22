package client

import (
	"math"
	"testing"
	"time"
)

func TestTrafficSampleIntegrityGuards(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	first, _ := repo.Create(Client{Name: "first", Enabled: true, QuotaResetPolicy: ResetNever})
	second, _ := repo.Create(Client{Name: "second", Enabled: true, QuotaResetPolicy: ResetNever})
	binding, _ := repo.CreateBinding(Binding{ClientID: first.ID, InboundID: "hy2", Enabled: true})

	for _, sample := range []Sample{
		{ClientID: first.ID, UploadBytes: -1, AtUnix: 1},
		{ClientID: first.ID, ProviderKey: "../bad", Monotonic: true, AtUnix: 1},
		{ClientID: "missing-client", UploadBytes: 1, AtUnix: 1},
		{ClientID: first.ID, UploadBytes: 1, AtUnix: time.Now().Add(time.Hour).Unix()},
		{BindingID: binding.ID, UploadBytes: 1, AtUnix: -1},
		{BindingID: binding.ID, UploadBytes: 1, AtUnix: 1, Monotonic: true, ProviderKey: "../../bad"},
		{ClientID: second.ID, BindingID: binding.ID, UploadBytes: 1, AtUnix: 1},
	} {
		if err := store.RecordSample(sample); err == nil {
			t.Fatalf("accepted invalid sample: %+v", sample)
		}
	}
}

func TestTrafficMonotonicAsymmetricResetAndStaleTimestamp(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	client, _ := repo.Create(Client{Name: "client", Enabled: true, QuotaResetPolicy: ResetNever})
	binding, _ := repo.CreateBinding(Binding{ClientID: client.ID, InboundID: "hy2", Enabled: true})
	if err := store.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 100, DownloadBytes: 100, AtUnix: 10, Monotonic: true, ProviderKey: "provider:one"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 50, DownloadBytes: 150, AtUnix: 11, Monotonic: true, ProviderKey: "provider:one"}); err != nil {
		t.Fatal(err)
	}
	up, down, _ := store.TotalsForClient(client.ID)
	if up != 0 || down != 50 {
		t.Fatalf("asymmetric reset totals=%d/%d, want 0/50", up, down)
	}
	if err := store.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 60, DownloadBytes: 160, AtUnix: 9, Monotonic: true, ProviderKey: "provider:one"}); err == nil {
		t.Fatal("accepted stale provider timestamp")
	}
}

func TestTrafficRowsAreCleanedUpWithBindingAndClient(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	current, _ := repo.Create(Client{Name: "cleanup", Enabled: true, QuotaResetPolicy: ResetNever})
	binding, _ := repo.CreateBinding(Binding{ClientID: current.ID, InboundID: "hy2", Enabled: true})
	if err := store.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 1, AtUnix: 1, Monotonic: true, ProviderKey: "provider:" + binding.ID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteBinding(binding.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"traffic_counters", "traffic_samples", "traffic_runtime_state"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan rows in %s: count=%d err=%v", table, count, err)
		}
	}
}

func TestTrafficCounterOverflowRollsBack(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	client, _ := repo.Create(Client{Name: "client", Enabled: true, QuotaResetPolicy: ResetNever})
	binding, _ := repo.CreateBinding(Binding{ClientID: client.ID, InboundID: "hy2", Enabled: true})
	if _, err := db.Exec(`INSERT INTO traffic_counters(client_id,binding_id,upload_bytes,download_bytes,updated_at) VALUES(?,?,?,?,?)`, client.ID, binding.ID, int64(math.MaxInt64), 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSample(Sample{BindingID: binding.ID, UploadBytes: 1, AtUnix: 2}); err == nil {
		t.Fatal("expected counter overflow rejection")
	}
	up, _, _ := store.TotalsForClient(client.ID)
	if up != math.MaxInt64 {
		t.Fatalf("overflow mutated counter to %d", up)
	}
}
