package runtime

import (
	"sync"
	"time"
)

// ProcessCPUSampler keeps the previous (utime+stime ticks, wall-clock) sample
// for each PID so process cpuPercent is a live rate — Δticks/Δt between
// samples — rather than the since-start lifetime average that kept runaway
// daemons invisible on the System page (#1190). The API layer builds a fresh
// discovery reader per request, so the sampler state lives one level up and
// is shared across requests.
type ProcessCPUSampler struct {
	mu                  sync.Mutex
	clockTicksPerSecond int64
	now                 func() time.Time
	staleAfter          time.Duration
	samples             map[int]processCPUSample
}

type processCPUSample struct {
	ticks          int64
	startTimeTicks int64
	at             time.Time
}

// minSampleDelta is the shortest wall-clock gap trusted for a delta rate:
// two consumers sharing the sampler can land reads milliseconds apart, and
// dividing a small tick delta by a tiny interval would spike cpuPercent.
// Below the floor the previous-sample answer (lifetime average) stands.
const minSampleDelta = 100 * time.Millisecond

// NewProcessCPUSampler creates a sampler. now is injectable for tests; nil
// uses time.Now. clockTicksPerSecond defaults to the common USER_HZ of 100,
// which every supported Linux arch uses — a non-100 host would need
// sysconf(_SC_CLK_TCK), which is not reachable without cgo.
func NewProcessCPUSampler(clockTicksPerSecond int64, now func() time.Time) *ProcessCPUSampler {
	if clockTicksPerSecond <= 0 {
		clockTicksPerSecond = 100
	}
	if now == nil {
		now = time.Now
	}
	return &ProcessCPUSampler{
		clockTicksPerSecond: clockTicksPerSecond,
		now:                 now,
		staleAfter:          10 * time.Minute,
		samples:             map[int]processCPUSample{},
	}
}

// Percent records a fresh utime+stime sample for pid and returns the CPU rate
// since the previous sample: Δticks / CLK_TCK / Δwall × 100 (top-style — 100
// means one saturated core, so multithreaded processes can exceed 100). The
// first sample for a pid, a reused pid whose process start time changed
// (respawn), a stalled clock, or a backwards counter all fall back to the
// since-start lifetime average so the reported value stays a finite
// non-negative number for JSON consumers.
func (s *ProcessCPUSampler) Percent(pid int, stat ProcessStatFields, systemUptimeSeconds int64) float64 {
	if s == nil {
		return NewProcessCPUUsage(0).Percent(stat, systemUptimeSeconds)
	}
	fallback := NewProcessCPUUsage(s.clockTicksPerSecond).Percent(stat, systemUptimeSeconds)
	totalTicks := stat.UserTicks + stat.SystemTicks
	now := s.now()
	s.mu.Lock()
	prev, ok := s.samples[pid]
	s.samples[pid] = processCPUSample{ticks: totalTicks, startTimeTicks: stat.StartTimeTicks, at: now}
	s.pruneStale(now)
	s.mu.Unlock()
	if !ok || prev.startTimeTicks != stat.StartTimeTicks {
		return fallback
	}
	elapsed := now.Sub(prev.at)
	deltaTicks := totalTicks - prev.ticks
	if elapsed < minSampleDelta || deltaTicks < 0 {
		return fallback
	}
	return float64(deltaTicks) / float64(s.clockTicksPerSecond) / elapsed.Seconds() * 100
}

// pruneStale drops samples older than staleAfter so dead PIDs cannot grow the
// map unboundedly. Callers must hold s.mu.
func (s *ProcessCPUSampler) pruneStale(now time.Time) {
	for pid, sample := range s.samples {
		if now.Sub(sample.at) > s.staleAfter {
			delete(s.samples, pid)
		}
	}
}
