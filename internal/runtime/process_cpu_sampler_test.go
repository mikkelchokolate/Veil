package runtime

import (
	"sync"
	"testing"
	"time"
)

// samplerClock is a manually advanced wall clock for ProcessCPUSampler tests.
type samplerClock struct {
	t time.Time
}

func (c *samplerClock) now() time.Time { return c.t }

func (c *samplerClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestProcessCPUSamplerFirstSampleReportsLifetimeAverage(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	stat := ProcessStatFields{UserTicks: 1000, SystemTicks: 500, StartTimeTicks: 5000}
	want := NewProcessCPUUsage(100).Percent(stat, 200)
	if got := sampler.Percent(42, stat, 200); got != want {
		t.Fatalf("first sample percent = %v, want lifetime average %v", got, want)
	}
}

// Regression for #1190: a tick jump between two samples must surface as the
// delta rate (one saturated core = 100%), not the since-start lifetime average
// that hid a runaway daemon.
func TestProcessCPUSamplerReportsDeltaRateBetweenSamples(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	// Process up for 95 s of a 100 s uptime, having burned 10 CPU-seconds.
	first := ProcessStatFields{UserTicks: 700, SystemTicks: 300, StartTimeTicks: 500}
	sampler.Percent(42, first, 100)

	// 5 s later the process burned another 5 CPU-seconds (one full core).
	clock.advance(5 * time.Second)
	second := ProcessStatFields{UserTicks: 1200, SystemTicks: 300, StartTimeTicks: 500}
	got := sampler.Percent(42, second, 105)
	if got != 100 {
		t.Fatalf("delta rate percent = %v, want 100", got)
	}
	lifetime := NewProcessCPUUsage(100).Percent(second, 105)
	if got == lifetime {
		t.Fatalf("delta rate %v must differ from lifetime average %v", got, lifetime)
	}
}

// A pid that dies and is reused by a new process (starttime changed) must not
// produce a negative or otherwise garbage rate — the new process's first
// sample falls back to its own lifetime average.
func TestProcessCPUSamplerRespawnedPIDReportsLifetimeAverage(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	sampler.Percent(42, ProcessStatFields{UserTicks: 900, SystemTicks: 100, StartTimeTicks: 500}, 100)

	clock.advance(5 * time.Second)
	// Same pid, new process: counters restarted (10 ticks) with a later start.
	respawned := ProcessStatFields{UserTicks: 8, SystemTicks: 2, StartTimeTicks: 9000}
	want := NewProcessCPUUsage(100).Percent(respawned, 105)
	got := sampler.Percent(42, respawned, 105)
	if got != want {
		t.Fatalf("respawned pid percent = %v, want lifetime average %v", got, want)
	}
	if got < 0 {
		t.Fatalf("respawned pid percent must not be negative, got %v", got)
	}
}

// A backwards tick counter with an unchanged starttime (defensive — Linux
// counters are monotonic) must fall back, never report a negative rate.
func TestProcessCPUSamplerBackwardsTicksFallsBack(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	sampler.Percent(42, ProcessStatFields{UserTicks: 900, SystemTicks: 100, StartTimeTicks: 500}, 100)

	clock.advance(5 * time.Second)
	shrunk := ProcessStatFields{UserTicks: 100, SystemTicks: 50, StartTimeTicks: 500}
	want := NewProcessCPUUsage(100).Percent(shrunk, 105)
	if got := sampler.Percent(42, shrunk, 105); got != want {
		t.Fatalf("backwards-ticks percent = %v, want fallback %v", got, want)
	}
}

// Two samples at the same wall-clock instant have no measurable rate: fall
// back rather than divide by zero.
func TestProcessCPUSamplerNonPositiveElapsedFallsBack(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	stat := ProcessStatFields{UserTicks: 900, SystemTicks: 100, StartTimeTicks: 500}
	sampler.Percent(42, stat, 100)
	want := NewProcessCPUUsage(100).Percent(stat, 100)
	if got := sampler.Percent(42, stat, 100); got != want {
		t.Fatalf("zero-elapsed percent = %v, want fallback %v", got, want)
	}
}

// Each sample replaces the previous one, so rates always span the most recent
// pair of polls.
func TestProcessCPUSamplerRatesChainAcrossSamples(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	sampler.Percent(42, ProcessStatFields{UserTicks: 1000, StartTimeTicks: 500}, 100)

	clock.advance(10 * time.Second)
	// +100 ticks over 10 s at 100 tps = 10%.
	if got := sampler.Percent(42, ProcessStatFields{UserTicks: 1100, StartTimeTicks: 500}, 110); got != 10 {
		t.Fatalf("first delta percent = %v, want 10", got)
	}
	clock.advance(5 * time.Second)
	// +250 ticks over 5 s = 50%.
	if got := sampler.Percent(42, ProcessStatFields{UserTicks: 1350, StartTimeTicks: 500}, 115); got != 50 {
		t.Fatalf("second delta percent = %v, want 50", got)
	}
}

// A multithreaded process saturating two cores reports 200% (top-style), not
// a clamped 100%.
func TestProcessCPUSamplerRateCanExceedOneHundred(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	sampler.Percent(42, ProcessStatFields{UserTicks: 1000, StartTimeTicks: 500}, 100)

	clock.advance(5 * time.Second)
	// +1000 ticks over 5 s = 10 CPU-seconds over 5 wall seconds = 200%.
	if got := sampler.Percent(42, ProcessStatFields{UserTicks: 2000, StartTimeTicks: 500}, 105); got != 200 {
		t.Fatalf("multicore delta percent = %v, want 200", got)
	}
}

// Samples for pids that stop appearing are pruned after staleAfter so the map
// cannot grow unboundedly.
func TestProcessCPUSamplerPrunesStaleEntries(t *testing.T) {
	clock := &samplerClock{t: time.Unix(1_700_000_000, 0)}
	sampler := NewProcessCPUSampler(100, clock.now)
	sampler.staleAfter = time.Minute
	sampler.Percent(1, ProcessStatFields{UserTicks: 100, StartTimeTicks: 500}, 100)

	clock.advance(2 * time.Minute)
	sampler.Percent(2, ProcessStatFields{UserTicks: 100, StartTimeTicks: 500}, 200)

	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	if len(sampler.samples) != 1 {
		t.Fatalf("samples = %v, want only the fresh pid", sampler.samples)
	}
	if _, ok := sampler.samples[2]; !ok {
		t.Fatal("fresh pid sample was pruned")
	}
}

// A nil sampler (zero-value procProcessSource) keeps the legacy
// lifetime-average behavior.
func TestProcessCPUSamplerNilReturnsLifetimeAverage(t *testing.T) {
	var sampler *ProcessCPUSampler
	stat := ProcessStatFields{UserTicks: 1000, SystemTicks: 500, StartTimeTicks: 5000}
	want := NewProcessCPUUsage(100).Percent(stat, 200)
	if got := sampler.Percent(42, stat, 200); got != want {
		t.Fatalf("nil sampler percent = %v, want %v", got, want)
	}
}

func TestProcessCPUSamplerDefaultsInvalidArgs(t *testing.T) {
	sampler := NewProcessCPUSampler(0, nil)
	stat := ProcessStatFields{UserTicks: 100, SystemTicks: 50, StartTimeTicks: 1000}
	want := NewProcessCPUUsage(100).Percent(stat, 20)
	if got := sampler.Percent(42, stat, 20); got != want {
		t.Fatalf("percent = %v, want %v", got, want)
	}
}

// Concurrent polls (e.g. /api/processes and /api/runtime/observation) share
// the sampler; exercise it under -race.
func TestProcessCPUSamplerConcurrentAccess(t *testing.T) {
	sampler := NewProcessCPUSampler(100, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				sampler.Percent(pid, ProcessStatFields{UserTicks: int64(j), StartTimeTicks: 500}, 100)
			}
		}(i)
	}
	wg.Wait()
}
