package client

import (
	"sync"
	"testing"
	"time"
)

type blockingTrafficProvider struct {
	key     string
	binding string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingTrafficProvider) Key() string { return p.key }
func (p *blockingTrafficProvider) Read() (map[string]ProviderReading, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return map[string]ProviderReading{
		p.binding: {BindingID: p.binding, UploadBytes: 10, DownloadBytes: 20},
	}, nil
}

func TestCollectorStopWaitsForInFlightCollection(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	row, err := repo.Create(Client{Name: "collector-stop", Enabled: true, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.CreateBinding(Binding{ClientID: row.ID, InboundID: "stop", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	provider := &blockingTrafficProvider{
		key: "stop:" + binding.ID, binding: binding.ID,
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	collector := NewCollector(NewTrafficStore(db), time.Millisecond, nil)
	collector.ResetProviders([]TrafficProvider{provider})
	collector.Start()
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("collector did not enter provider read")
	}
	stopped := make(chan struct{})
	go func() {
		collector.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Collector.Stop returned before in-flight collection completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(provider.release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Collector.Stop did not join worker")
	}
	if collector.Running() {
		t.Fatal("collector still reports running after Stop")
	}
}

func TestReconcilerStopWaitsForInFlightTransition(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := NewRepository(db)
	quota := int64(1)
	row, err := repo.Create(Client{Name: "reconciler-stop", Enabled: true, QuotaBytes: &quota, QuotaResetPolicy: ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewTrafficStore(db).RecordSample(Sample{ClientID: row.ID, UploadBytes: 1, AtUnix: 1}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	reconciler := NewReconciler(repo, NewTrafficStore(db), time.Millisecond, func(string, bool) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	})
	reconciler.Start()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not enter transition callback")
	}
	stopped := make(chan struct{})
	go func() {
		reconciler.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Reconciler.Stop returned before in-flight transition completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Reconciler.Stop did not join worker")
	}
	if reconciler.Running() {
		t.Fatal("reconciler still reports running after Stop")
	}
}
