package observability

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type RateLimiterEngine struct {
	buckets *sync.Map
	// count approximates len(buckets) so cap checks stay O(1); it is only a
	// bound, and a rare race may leave it off by a small delta.
	count      atomic.Int64
	maxBuckets int
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	mu       sync.Mutex
}

func NewRateLimiterEngine() *RateLimiterEngine {
	return newRateLimiterEngineWithBuckets(&sync.Map{})
}

// NewBoundedRateLimiterEngine creates an engine that never holds more than
// maxBuckets buckets: when a new bucket would exceed the cap, stale buckets
// (oldest lastTime first) are evicted instead. This bounds memory under
// unauthenticated spray that mints a fresh key per request (#997).
func NewBoundedRateLimiterEngine(maxBuckets int) *RateLimiterEngine {
	engine := newRateLimiterEngineWithBuckets(&sync.Map{})
	engine.maxBuckets = maxBuckets
	return engine
}

func newRateLimiterEngineWithBuckets(buckets *sync.Map) *RateLimiterEngine {
	return &RateLimiterEngine{buckets: buckets}
}

func (e *RateLimiterEngine) Allow(key string, rate float64, burst int) (bool, time.Duration) {
	val, loaded := e.buckets.LoadOrStore(key, &tokenBucket{
		tokens:   float64(burst),
		lastTime: time.Now(),
	})
	if !loaded {
		e.count.Add(1)
		e.enforceBucketCap(key)
	}
	tb := val.(*tokenBucket)
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.tokens += elapsed * rate
	if tb.tokens > float64(burst) {
		tb.tokens = float64(burst)
	}
	tb.lastTime = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true, 0
	}
	needed := 1.0 - tb.tokens
	retryAfter := time.Duration(needed / rate * float64(time.Second))
	return false, retryAfter
}

// Size returns the approximate number of tracked buckets.
func (e *RateLimiterEngine) Size() int {
	return int(e.count.Load())
}

// enforceBucketCap evicts the stalest buckets (oldest lastTime first) until
// the map fits maxBuckets. keepKey is never evicted — it is the bucket the
// caller just created for its own request. Eviction only discards tracked
// budget; it never fails a request.
func (e *RateLimiterEngine) enforceBucketCap(keepKey string) {
	if e.maxBuckets <= 0 || e.count.Load() <= int64(e.maxBuckets) {
		return
	}
	type candidate struct {
		key      any
		lastTime time.Time
	}
	var candidates []candidate
	e.buckets.Range(func(key, value any) bool {
		if key == keepKey {
			return true
		}
		tb := value.(*tokenBucket)
		tb.mu.Lock()
		candidates = append(candidates, candidate{key: key, lastTime: tb.lastTime})
		tb.mu.Unlock()
		return true
	})
	// Oldest-idle buckets go first: stale entries are freed before any
	// recently active budget is dropped.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].lastTime.Before(candidates[j].lastTime) })
	for _, item := range candidates {
		if e.count.Load() <= int64(e.maxBuckets) {
			break
		}
		e.buckets.Delete(item.key)
		e.count.Add(-1)
	}
}

func (e *RateLimiterEngine) Cleanup(cutoff time.Time) {
	e.buckets.Range(func(key, value any) bool {
		tb := value.(*tokenBucket)
		tb.mu.Lock()
		lastTime := tb.lastTime
		tb.mu.Unlock()
		if lastTime.Before(cutoff) {
			e.buckets.Delete(key)
			e.count.Add(-1)
		}
		return true
	})
}
