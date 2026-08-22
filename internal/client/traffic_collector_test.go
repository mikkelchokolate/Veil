package client

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"
)

type fakeProvider struct {
	key      string
	readings map[string]ProviderReading
}

func (f *fakeProvider) Key() string { return f.key }
func (f *fakeProvider) Read() (ProviderBatch, error) {
	readings := make([]ProviderReading, 0, len(f.readings))
	for _, reading := range f.readings {
		readings = append(readings, reading)
	}
	return ProviderBatch{Readings: readings, ObservedAt: time.Now().UTC(), RuntimeInstance: f.key}, nil
}

type batchProvider struct {
	key   string
	batch ProviderBatch
}

func (p *batchProvider) Key() string                                        { return p.key }
func (p *batchProvider) Read() (ProviderBatch, error)                       { return p.batch, nil }
func (p *batchProvider) ReadContext(context.Context) (ProviderBatch, error) { return p.batch, nil }

func TestUnknownHysteriaIdentityDoesNotDiscardValidBatchReadings(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	row, err := repo.Create(Client{Name: "mixed-identities", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "hy-mixed", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	provider := &batchProvider{key: "hysteria2:hy-mixed"}
	provider.batch = ProviderBatch{
		Readings:          []ProviderReading{{BindingID: binding.ID, UploadBytes: 100, DownloadBytes: 200}},
		UnknownIdentities: []string{"foreign-user"}, ObservedAt: time.Now().UTC(), RuntimeInstance: provider.key,
	}
	collector := NewCollector(store, time.Second, nil)
	if err := collector.Register(provider); err != nil {
		t.Fatal(err)
	}
	if err := collector.CollectOnce(); err == nil || !strings.Contains(err.Error(), "unknown runtime identities") {
		t.Fatalf("first mixed batch error=%v", err)
	}
	provider.batch.Readings[0].UploadBytes = 160
	provider.batch.Readings[0].DownloadBytes = 260
	provider.batch.ObservedAt = time.Now().UTC()
	if err := collector.CollectOnce(); err == nil {
		t.Fatal("mixed batch should keep provider degraded")
	}
	up, down, err := store.TotalsForClient(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 60 || down != 60 {
		t.Fatalf("valid reading was discarded: totals=%d/%d", up, down)
	}
	status := collector.ProviderHealth()
	if len(status) != 1 || status[0].State != "degraded" {
		t.Fatalf("provider health=%+v", status)
	}
}

func TestCollectorAttributesSamplesToClients(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	ts := NewTrafficStore(db)

	c, _ := repo.Create(Client{Name: "col", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-1", Enabled: true})

	prov := &fakeProvider{key: "hy2", readings: map[string]ProviderReading{
		b.ID: {BindingID: b.ID, UploadBytes: 500, DownloadBytes: 700},
	}}
	col := NewCollector(ts, 0, nil)
	col.Register(prov)

	// First collect establishes baseline (no delta).
	if err := col.CollectOnce(); err != nil {
		t.Fatalf("collect1: %v", err)
	}
	// Advance the provider counters and collect again.
	prov.readings[b.ID] = ProviderReading{BindingID: b.ID, UploadBytes: 800, DownloadBytes: 1000}
	if err := col.CollectOnce(); err != nil {
		t.Fatalf("collect2: %v", err)
	}
	up, down, _ := ts.TotalsForClient(c.ID)
	if up != 300 || down != 300 {
		t.Fatalf("collected totals = up %d down %d, want 300/300 (delta)", up, down)
	}
}

func TestCollectorContinuesPastBrokenProviderReportsAndRateLimitsFailure(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	row, err := repo.Create(Client{Name: "healthy-after-broken", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "healthy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	healthy := &fakeProvider{key: "healthy", readings: map[string]ProviderReading{
		binding.ID: {BindingID: binding.ID, UploadBytes: 100, DownloadBytes: 100},
	}}
	collector := NewCollector(store, 0, nil)
	collector.Register(&brokenProvider{key: "bad"})
	collector.Register(healthy)

	var logs bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	for i := 0; i < 3; i++ {
		if err := collector.CollectOnce(); err == nil || !strings.Contains(err.Error(), "bad") {
			t.Errorf("collection %d error = %v, want aggregate provider error", i+1, err)
		}
		healthy.readings[binding.ID] = ProviderReading{
			BindingID: binding.ID, UploadBytes: int64(200 + 100*i), DownloadBytes: int64(200 + 100*i),
		}
	}
	up, down, err := store.TotalsForClient(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up == 0 || down == 0 {
		t.Errorf("healthy provider was blocked by broken provider: totals=%d/%d", up, down)
	}
	if got := strings.Count(logs.String(), "event=traffic_provider_failure"); got != 1 {
		t.Errorf("rate-limited structured provider logs = %d, want 1; logs=%q", got, logs.String())
	}
}

func TestCollectorReportsTrafficStoreWriteFailure(t *testing.T) {
	db := openTestDB(t)
	repo := NewRepository(db)
	store := NewTrafficStore(db)
	row, err := repo.Create(Client{Name: "store-failure", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "store", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{key: "store-provider", readings: map[string]ProviderReading{
		binding.ID: {BindingID: binding.ID, UploadBytes: 100, DownloadBytes: 100},
	}}
	collector := NewCollector(store, 0, nil)
	collector.Register(provider)
	if err := collector.CollectOnce(); err != nil {
		t.Fatalf("establish baseline: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	provider.readings[binding.ID] = ProviderReading{BindingID: binding.ID, UploadBytes: 200, DownloadBytes: 200}
	if err := collector.CollectOnce(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "record") {
		t.Fatalf("store write error = %v, want reported RecordSample failure", err)
	}
}

type brokenProvider struct{ key string }

func (b *brokenProvider) Key() string { return b.key }
func (b *brokenProvider) Read() (ProviderBatch, error) {
	return ProviderBatch{}, errBrokenProvider
}

var errBrokenProvider = errTest("provider down")

type errTest string

func (e errTest) Error() string { return string(e) }
