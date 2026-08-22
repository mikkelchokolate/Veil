package client

import (
	"sync"
	"testing"
)

func TestRecordMonotonicSampleConcurrentEqualReadingWritesOneDelta(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	row, err := repo.Create(Client{Name: "concurrent-monotonic", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "provider", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	baseline := Sample{
		BindingID: binding.ID, ProviderKey: "provider:" + binding.ID,
		UploadBytes: 100, DownloadBytes: 200, AtUnix: 1, Monotonic: true,
	}
	if err := traffic.RecordSample(baseline); err != nil {
		t.Fatal(err)
	}

	const callers = 32
	start := make(chan struct{})
	errs := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			ready.Done()
			<-start
			errs <- traffic.RecordSample(Sample{
				BindingID: binding.ID, ProviderKey: "provider:" + binding.ID,
				UploadBytes: 200, DownloadBytes: 400, AtUnix: 2, Monotonic: true,
			})
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent record %d: %v", i, err)
		}
	}
	up, down, err := traffic.TotalsForClient(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 100 || down != 200 {
		t.Fatalf("concurrent equal reading recorded totals=%d/%d, want one delta 100/200", up, down)
	}
}

func TestConcurrentCollectOnceSameProviderReadingWritesOneDelta(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	traffic := NewTrafficStore(db)
	row, err := repo.Create(Client{Name: "concurrent-collector", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "provider", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{key: "concurrent-provider", readings: map[string]ProviderReading{
		binding.ID: {BindingID: binding.ID, UploadBytes: 100, DownloadBytes: 200},
	}}
	collector := NewCollector(traffic, 0, nil)
	collector.Register(provider)
	if err := collector.CollectOnce(); err != nil {
		t.Fatal(err)
	}
	provider.readings = map[string]ProviderReading{
		binding.ID: {BindingID: binding.ID, UploadBytes: 200, DownloadBytes: 400},
	}

	const callers = 32
	start := make(chan struct{})
	errs := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			ready.Done()
			<-start
			errs <- collector.CollectOnce()
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent CollectOnce %d: %v", i, err)
		}
	}
	up, down, err := traffic.TotalsForClient(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 100 || down != 200 {
		t.Fatalf("concurrent CollectOnce totals=%d/%d, want one delta 100/200", up, down)
	}
}
