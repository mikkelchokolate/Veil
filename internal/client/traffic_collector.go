package client

import (
	"sync"
	"time"
)

// TrafficProvider reads absolute byte counters from a live runtime (e.g. a
// hysteria2 / naiveproxy process) keyed by binding. Implementations are
// protocol-specific; the collector is protocol-agnostic.
type TrafficProvider interface {
	// Key identifies this provider instance (used for monotonic runtime state).
	Key() string
	// Read returns absolute (monotonic) upload/download counters per binding ID.
	Read() (map[string]ProviderReading, error)
}

// ProviderReading is one binding's absolute counters from a provider.
type ProviderReading struct {
	BindingID     string
	UploadBytes   int64
	DownloadBytes int64
}

// Collector polls registered providers on an interval, recording monotonic
// samples into the store and reporting quota/expiry state changes via a
// callback. It is safe for concurrent use.
type Collector struct {
	store     *TrafficStore
	providers []TrafficProvider
	interval  time.Duration
	onExhaust func(clientID string) // called when a client crosses its quota

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	done    chan struct{}
}

func NewCollector(store *TrafficStore, interval time.Duration, onExhaust func(clientID string)) *Collector {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Collector{store: store, interval: interval, onExhaust: onExhaust}
}

// Register adds a provider. Thread-safe; typically called at startup.
func (c *Collector) Register(p TrafficProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = append(c.providers, p)
}

// ResetProviders replaces the registered provider set. Used when clients,
// bindings, credentials, or inbounds change so provider attribution stays
// accurate without a restart.
func (c *Collector) ResetProviders(providers []TrafficProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = append([]TrafficProvider(nil), providers...)
}

// CollectOnce reads all providers and records samples. Exposed for tests and
// for an immediate manual reconcile.
func (c *Collector) CollectOnce() error {
	c.mu.Lock()
	providers := append([]TrafficProvider(nil), c.providers...)
	c.mu.Unlock()
	now := time.Now().Unix()
	for _, p := range providers {
		readings, err := p.Read()
		if err != nil {
			continue // a broken provider must not stall others
		}
		for _, rd := range readings {
			_ = c.store.RecordSample(Sample{
				BindingID:     rd.BindingID,
				UploadBytes:   rd.UploadBytes,
				DownloadBytes: rd.DownloadBytes,
				AtUnix:        now,
				Monotonic:     true,
				ProviderKey:   p.Key() + ":" + rd.BindingID,
			})
		}
	}
	return nil
}

// ProviderCount reports how many traffic providers are registered. Zero means
// no runtime is feeding counters and the telemetry state must be reported as
// "no providers" rather than fake zeros.
func (c *Collector) ProviderCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.providers)
}

// Start begins periodic collection until Stop. Non-blocking.
func (c *Collector) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	stop := make(chan struct{})
	done := make(chan struct{})
	c.stop = stop
	c.done = done
	c.mu.Unlock()
	go func() {
		defer close(done)
		t := time.NewTicker(c.interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				_ = c.CollectOnce()
			}
		}
	}()
}

// Stop halts periodic collection.
func (c *Collector) Stop() {
	c.mu.Lock()
	if !c.running {
		done := c.done
		c.mu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	c.running = false
	stop := c.stop
	done := c.done
	close(stop)
	c.mu.Unlock()
	<-done
	c.mu.Lock()
	if c.stop == stop {
		c.stop = nil
		c.done = nil
	}
	c.mu.Unlock()
}

// Running reports whether the periodic collection loop is active.
func (c *Collector) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}
