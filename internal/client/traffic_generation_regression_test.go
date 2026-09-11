package client

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingProvider struct {
	key     string
	started chan struct{}
	release chan struct{}
	err     error
}

func (p *blockingProvider) Key() string { return p.key }
func (p *blockingProvider) Read() (ProviderBatch, error) {
	return p.ReadContext(context.Background())
}
func (p *blockingProvider) ReadContext(context.Context) (ProviderBatch, error) {
	close(p.started)
	<-p.release
	if p.err != nil {
		return ProviderBatch{}, p.err
	}
	return ProviderBatch{ObservedAt: time.Now().UTC(), RuntimeInstance: p.key}, nil
}

func TestRemovedProviderLateFailureDoesNotResurrectHealth(t *testing.T) {
	collector := NewCollector(nil, time.Hour, nil)
	oldProvider := &blockingProvider{key: "old", started: make(chan struct{}), release: make(chan struct{}), err: context.DeadlineExceeded}
	newProvider := &batchProvider{key: "new", batch: ProviderBatch{ObservedAt: time.Now().UTC(), RuntimeInstance: "new"}}
	if err := collector.ResetProductionProviders([]TrafficProvider{oldProvider}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = collector.CollectOnceContext(context.Background())
	}()
	<-oldProvider.started
	if err := collector.ResetProductionProviders([]TrafficProvider{newProvider}); err != nil {
		t.Fatal(err)
	}
	close(oldProvider.release)
	wg.Wait()

	health := collector.ProviderHealth()
	for _, entry := range health {
		if entry.Key == "old" {
			t.Fatalf("removed provider health resurrected: %+v", health)
		}
	}
}
