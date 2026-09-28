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

// TrafficProvider reads one validated batch of absolute byte counters.
type TrafficProvider interface {
	Key() string
	Read() (ProviderBatch, error)
}

type ContextTrafficProvider interface {
	ReadContext(context.Context) (ProviderBatch, error)
}

type ProviderBatch struct {
	Readings          []ProviderReading
	UnknownIdentities []string
	ObservedAt        time.Time
	RuntimeInstance   string
	// RuntimeInstanceID identifies the OS process instance that produced the
	// absolute counters (e.g. systemd start timestamp + main PID). Empty when
	// the provider cannot determine it — accounting then falls back to the
	// negative-delta reset heuristic (#1102).
	RuntimeInstanceID string
	// Online is the runtime's authoritative live-session table when it
	// exposes one (hysteria2 serves GET /online on the trafficStats
	// listener): bindingID -> live connection count, merged across aliased
	// identities exactly like Readings. nil means the provider has no
	// presence surface or the read failed — presence consumers fall back to
	// the counter-activity heuristic rather than assuming everyone is
	// offline. A non-nil empty map is an authoritative "nobody is online".
	Online map[string]int64
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

// providerPresence caches the latest live-session table one provider
// reported. A nil online map means the provider observed successfully but
// has no presence surface (mieru) or the authoritative read failed —
// consumers must fall back to activity, not report offline.
type providerPresence struct {
	observedAt int64
	online     map[string]int64
}

// ProviderPresence is one provider's last successfully collected
// live-session snapshot. Online is keyed by bindingID; nil means no
// presence data was reported that round.
type ProviderPresence struct {
	ObservedAt int64
	Online     map[string]int64
}

type Collector struct {
	store     *TrafficStore
	providers []TrafficProvider
	interval  time.Duration
	onExhaust func(clientID string)

	mu         sync.Mutex
	collectMu  sync.Mutex
	generation uint64
	health     map[string]providerHealthState
	presence   map[string]providerPresence
	running    bool
	stop       chan struct{}
	done       chan struct{}
	cancel     context.CancelFunc
}

func NewCollector(store *TrafficStore, interval time.Duration, onExhaust func(clientID string)) *Collector {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Collector{store: store, interval: interval, onExhaust: onExhaust, health: make(map[string]providerHealthState), presence: make(map[string]providerPresence)}
}

func (c *Collector) Register(provider TrafficProvider) error {
	if provider == nil || strings.TrimSpace(provider.Key()) == "" {
		return errors.New("traffic provider key is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, existing := range c.providers {
		if existing.Key() == provider.Key() {
			return fmt.Errorf("duplicate traffic provider key %q", provider.Key())
		}
	}
	c.providers = append(c.providers, provider)
	c.ensureProviderHealthLocked(provider.Key())
	return nil
}

func (c *Collector) ResetProductionProviders(providers []TrafficProvider) error {
	for _, provider := range providers {
		if _, ok := provider.(ContextTrafficProvider); !ok {
			return fmt.Errorf("traffic provider %q is not context-aware", provider.Key())
		}
	}
	return c.ResetProviders(providers)
}

func (c *Collector) ResetProviders(providers []TrafficProvider) error {
	current := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if provider == nil || strings.TrimSpace(provider.Key()) == "" {
			return errors.New("traffic provider key is required")
		}
		if _, duplicate := current[provider.Key()]; duplicate {
			return fmt.Errorf("duplicate traffic provider key %q", provider.Key())
		}
		current[provider.Key()] = struct{}{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.providers = append([]TrafficProvider(nil), providers...)
	for _, provider := range providers {
		c.ensureProviderHealthLocked(provider.Key())
	}
	for key := range c.health {
		if _, ok := current[key]; !ok {
			delete(c.health, key)
		}
	}
	for key := range c.presence {
		if _, ok := current[key]; !ok {
			delete(c.presence, key)
		}
	}
	return nil
}

func (c *Collector) ensureProviderHealthLocked(key string) {
	if _, ok := c.health[key]; !ok {
		c.health[key] = providerHealthState{ProviderHealth: ProviderHealth{Key: key, State: "unknown"}}
	}
}

func validateProviderBatch(batch ProviderBatch) error {
	if batch.ObservedAt.IsZero() || batch.ObservedAt.After(time.Now().Add(5*time.Minute)) {
		return errors.New("traffic provider batch has invalid observation time")
	}
	if !providerKeyPattern.MatchString(batch.RuntimeInstance) {
		return errors.New("traffic provider batch has invalid runtime instance")
	}
	seenReadings := make(map[string]struct{}, len(batch.Readings))
	for _, reading := range batch.Readings {
		if reading.BindingID == "" || reading.UploadBytes < 0 || reading.DownloadBytes < 0 {
			return errors.New("traffic provider batch has invalid reading")
		}
		if _, duplicate := seenReadings[reading.BindingID]; duplicate {
			return errors.New("traffic provider batch has duplicate binding")
		}
		seenReadings[reading.BindingID] = struct{}{}
	}
	seenUnknown := make(map[string]struct{}, len(batch.UnknownIdentities))
	for _, identity := range batch.UnknownIdentities {
		if strings.TrimSpace(identity) == "" {
			return errors.New("traffic provider batch has empty unknown identity")
		}
		if _, duplicate := seenUnknown[identity]; duplicate {
			return errors.New("traffic provider batch has duplicate unknown identity")
		}
		seenUnknown[identity] = struct{}{}
	}
	for bindingID, count := range batch.Online {
		if bindingID == "" || count < 0 {
			return errors.New("traffic provider batch has invalid online reading")
		}
	}
	return nil
}

func (c *Collector) CollectOnce() error {
	return c.CollectOnceContext(context.Background())
}

func (c *Collector) CollectOnceContext(ctx context.Context) error {
	c.collectMu.Lock()
	defer c.collectMu.Unlock()

	c.mu.Lock()
	providers := append([]TrafficProvider(nil), c.providers...)
	generation := c.generation
	c.mu.Unlock()
	now := time.Now().Unix()
	var collectionErrors []error
	for _, provider := range providers {
		var batch ProviderBatch
		var err error
		if contextual, ok := provider.(ContextTrafficProvider); ok {
			providerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			batch, err = contextual.ReadContext(providerCtx)
			cancel()
		} else {
			batch, err = provider.Read()
		}
		if err == nil {
			err = validateProviderBatch(batch)
		}
		if err == nil {
			if c.store == nil {
				err = errors.New("traffic store is unavailable")
			} else {
				samples := make([]Sample, 0, len(batch.Readings))
				for _, reading := range batch.Readings {
					samples = append(samples, Sample{
						BindingID: reading.BindingID, UploadBytes: reading.UploadBytes, DownloadBytes: reading.DownloadBytes,
						AtUnix: batch.ObservedAt.Unix(), Monotonic: true,
						ProviderKey:       batch.RuntimeInstance + ":" + reading.BindingID,
						RuntimeInstanceID: batch.RuntimeInstanceID,
					})
				}
				if recordErr := c.store.RecordSamples(samples); recordErr != nil {
					err = fmt.Errorf("record provider %s batch: %w", provider.Key(), recordErr)
				}
			}
		}
		if err == nil && len(batch.UnknownIdentities) > 0 {
			log.Printf("event=traffic_unknown_identities provider=%q identities=%q", provider.Key(), strings.Join(batch.UnknownIdentities, ","))
		}
		if err != nil {
			wrapped := fmt.Errorf("traffic provider %s: %w", provider.Key(), err)
			collectionErrors = append(collectionErrors, wrapped)
			c.recordFailure(provider.Key(), wrapped, now, generation)
			continue
		}
		c.recordSuccess(provider.Key(), now, generation)
		c.recordPresence(provider.Key(), batch, generation)
	}
	return errors.Join(collectionErrors...)
}

func (c *Collector) MarkDegraded(key string, err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	generation := c.generation
	c.mu.Unlock()
	c.recordFailure(key, err, time.Now().Unix(), generation)
}

func (c *Collector) recordSuccess(key string, now int64, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	c.ensureProviderHealthLocked(key)
	state := c.health[key]
	state.State = "healthy"
	state.LastSuccessfulObservationAt = now
	state.LastError = ""
	c.health[key] = state
}

func (c *Collector) recordFailure(key string, err error, now int64, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
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

// recordPresence stores the batch's live-session table as the provider's
// latest presence snapshot. A nil Online map still records the observation:
// it means "no authoritative presence data this round" and ages out through
// the consumer's staleness bound rather than lingering as a fake answer.
func (c *Collector) recordPresence(key string, batch ProviderBatch, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	if c.presence == nil {
		c.presence = make(map[string]providerPresence)
	}
	c.presence[key] = providerPresence{observedAt: batch.ObservedAt.Unix(), online: batch.Online}
}

// PresenceSnapshot returns each provider's most recent live-session table,
// keyed by provider key (e.g. "hysteria2:<inbound>"). Entries are replaced
// wholesale on every successful collect and pruned by ResetProviders, so the
// snapshot can never merge data from an already-replaced registration.
func (c *Collector) PresenceSnapshot() map[string]ProviderPresence {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]ProviderPresence, len(c.presence))
	for key, p := range c.presence {
		var online map[string]int64
		if p.online != nil {
			online = make(map[string]int64, len(p.online))
			for bindingID, count := range p.online {
				online[bindingID] = count
			}
		}
		out[key] = ProviderPresence{ObservedAt: p.observedAt, Online: online}
	}
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
	generation := c.generation
	close(stop)
	if cancel != nil {
		cancel()
	}
	c.mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		c.recordFailure("collector", errors.New("traffic provider shutdown timed out"), time.Now().Unix(), generation)
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
