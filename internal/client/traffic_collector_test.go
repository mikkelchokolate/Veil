package client

import "testing"

type fakeProvider struct {
	key      string
	readings map[string]ProviderReading
}

func (f *fakeProvider) Key() string { return f.key }
func (f *fakeProvider) Read() (map[string]ProviderReading, error) {
	return f.readings, nil
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

func TestCollectorSurvivesBrokenProvider(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ts := NewTrafficStore(db)
	broken := &brokenProvider{key: "bad"}
	col := NewCollector(ts, 0, nil)
	col.Register(broken)
	if err := col.CollectOnce(); err != nil {
		t.Fatalf("broken provider must not fail collection: %v", err)
	}
}

type brokenProvider struct{ key string }

func (b *brokenProvider) Key() string { return b.key }
func (b *brokenProvider) Read() (map[string]ProviderReading, error) {
	return nil, errBrokenProvider
}

var errBrokenProvider = errTest("provider down")

type errTest string

func (e errTest) Error() string { return string(e) }
