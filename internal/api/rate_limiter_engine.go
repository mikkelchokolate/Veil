package api

import (
	"sync"
	"time"
)

type RateLimiterEngine struct {
	buckets *sync.Map
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	mu       sync.Mutex
}

func NewRateLimiterEngine() *RateLimiterEngine {
	return newRateLimiterEngineWithBuckets(&sync.Map{})
}

func newRateLimiterEngineWithBuckets(buckets *sync.Map) *RateLimiterEngine {
	return &RateLimiterEngine{buckets: buckets}
}

func (e *RateLimiterEngine) Allow(key string, rate float64, burst int) (bool, time.Duration) {
	val, _ := e.buckets.LoadOrStore(key, &tokenBucket{
		tokens:   float64(burst),
		lastTime: time.Now(),
	})
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

func (e *RateLimiterEngine) Cleanup(cutoff time.Time) {
	e.buckets.Range(func(key, value any) bool {
		tb := value.(*tokenBucket)
		tb.mu.Lock()
		lastTime := tb.lastTime
		tb.mu.Unlock()
		if lastTime.Before(cutoff) {
			e.buckets.Delete(key)
		}
		return true
	})
}
