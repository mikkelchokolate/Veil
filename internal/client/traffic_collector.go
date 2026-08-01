package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrafficProvider reads absolute byte counters from a live runtime keyed by binding.
type TrafficProvider interface {
	Key() string
	Read() (map[string]ProviderReading, error)
}

type ContextTrafficProvider interface {
	ReadContext(context.Context) (map[string]ProviderReading, error)
}

type ProviderReading struct {
	BindingID     string
	UploadBytes   int64
	DownloadBytes int64
}

type ProviderHealth struct {
	Key                         string `json:"key"`
	State                       string `json:"state"`
	LastSuccessfulObservationAt int64  `json:"lastSuccessfulObservationAt,omitempty"`
	LastError                   string `json:"lastError,omitempty"`
	ErrorsTotal                 uint64 `json:"errorsTotal"`
}

type providerHealthState struct {
	ProviderHealth
	lastLogAt int64
}

type Collector struct {
	store     *TrafficStore
	providers []TrafficProvider
	interval  time.Duration
	onExhaust func(clientID string)

	mu        sync.Mutex
	collectMu sync.Mutex
	health    map[string]providerHealthState
	running   bool
	stop      chan struct{}
	done      chan struct{}
	cancel    context.CancelFunc
}

func NewCollector(store *TrafficStore, interval time.Duration, onExhaust func(clientID string)) *Collector {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Collector{store: store, interval: interval, onExhaust: onExhaust, health: make(map[string]providerHealthState)}
}

func (c *Collector) Register(provider TrafficProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = append(c.providers, provider)
	c.ensureProviderHealthLocked(provider.Key())
}

func (c *Collector) ResetProviders(providers []TrafficProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = append([]TrafficProvider(nil), providers...)
	current := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		current[provider.Key()] = struct{}{}
		c.ensureProviderHealthLocked(provider.Key())
	}
	for key := range c.health {
		if _, ok := current[key]; !ok {
			delete(c.health, key)
		}
	}
}

func (c *Collector) ensureProviderHealthLocked(key string) {
	if _, ok := c.health[key]; !ok {
		c.health[key] = providerHealthState{ProviderHealth: ProviderHealth{Key: key, State: "unknown"}}
	}
}

func (c *Collector) CollectOnce() error {
	return c.CollectOnceContext(context.Background())
}

func (c *Collector) CollectOnceContext(ctx context.Context) error {
	c.collectMu.Lock()
	defer c.collectMu.Unlock()

	c.mu.Lock()
	providers := append([]TrafficProvider(nil), c.providers...)
	c.mu.Unlock()
	now := time.Now().Unix()
	var collectionErrors []error
	for _, provider := range providers {
		var readings map[string]ProviderReading
		var err error
		if contextual, ok := provider.(ContextTrafficProvider); ok {
			providerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			readings, err = contextual.ReadContext(providerCtx)
			cancel()
		} else {
			readings, err = provider.Read()
		}
		if err == nil {
			for _, reading := range readings {
				if c.store == nil {
					err = errors.New("traffic store is unavailable")
					break
				}
				if recordErr := c.store.RecordSample(Sample{
					BindingID: reading.BindingID, UploadBytes: reading.UploadBytes, DownloadBytes: reading.DownloadBytes,
					AtUnix: now, Monotonic: true, ProviderKey: provider.Key() + ":" + reading.BindingID,
				}); recordErr != nil {
					err = fmt.Errorf("record provider %s binding %s sample: %w", provider.Key(), reading.BindingID, recordErr)
					break
				}
			}
		}
		if err != nil {
			wrapped := fmt.Errorf("traffic provider %s: %w", provider.Key(), err)
			collectionErrors = append(collectionErrors, wrapped)
			c.recordFailure(provider.Key(), wrapped, now)
			continue
		}
		c.recordSuccess(provider.Key(), now)
	}
	return errors.Join(collectionErrors...)
}

func (c *Collector) recordSuccess(key string, now int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureProviderHealthLocked(key)
	state := c.health[key]
	state.State = "healthy"
	state.LastSuccessfulObservationAt = now
	state.LastError = ""
	c.health[key] = state
}

func (c *Collector) recordFailure(key string, err error, now int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureProviderHealthLocked(key)
	state := c.health[key]
	state.State = "degraded"
	state.LastError = err.Error()
	state.ErrorsTotal++
	if state.lastLogAt == 0 || now-state.lastLogAt >= 60 {
		log.Printf("event=traffic_provider_failure provider=%q error=%q", key, err.Error())
		state.lastLogAt = now
	}
	c.health[key] = state
}

func (c *Collector) ProviderHealth() []ProviderHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ProviderHealth, 0, len(c.health))
	for _, state := range c.health {
		out = append(out, state.ProviderHealth)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (c *Collector) PrometheusMetrics() string {
	var builder strings.Builder
	for _, status := range c.ProviderHealth() {
		key := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(status.Key)
		up := 0
		if status.State == "healthy" {
			up = 1
		}
		fmt.Fprintf(&builder, "veil_traffic_provider_up{provider=\"%s\"} %d\n", key, up)
		fmt.Fprintf(&builder, "veil_traffic_provider_errors_total{provider=\"%s\"} %d\n", key, status.ErrorsTotal)
		fmt.Fprintf(&builder, "veil_traffic_provider_last_success_unixtime{provider=\"%s\"} %d\n", key, status.LastSuccessfulObservationAt)
	}
	return builder.String()
}

func (c *Collector) ProviderCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.providers)
}

func (c *Collector) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	stop := make(chan struct{})
	done := make(chan struct{})
	workerCtx, cancel := context.WithCancel(context.Background())
	c.stop = stop
	c.done = done
	c.cancel = cancel
	c.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = c.CollectOnceContext(workerCtx)
			}
		}
	}()
}

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
	cancel := c.cancel
	close(stop)
	if cancel != nil {
		cancel()
	}
	c.mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.recordFailure("collector", errors.New("traffic provider shutdown timed out"), time.Now().Unix())
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	if c.stop == stop {
		c.stop = nil
		c.done = nil
		c.cancel = nil
	}
	c.mu.Unlock()
}

func (c *Collector) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}
