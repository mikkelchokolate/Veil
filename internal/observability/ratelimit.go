package observability

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EndpointLimit defines a custom rate limit for a specific path prefix.
type EndpointLimit struct {
	RatePerMinute int
	Burst         int
}

// RateLimiter is a per-IP token bucket rate limiter.
type RateLimiter struct {
	buckets        sync.Map
	engine         *RateLimiterEngine
	rate           float64
	burst          int
	endpointLimits map[string]EndpointLimit
	mu             sync.RWMutex
	stopCh         chan struct{}
	onRateLimited  func() // called when a request is rate-limited
}

// NewRateLimiter creates a new rate limiter with the given default rate and burst.
// ratePerMinute is the sustained request rate per minute per IP.
// burst is the maximum number of tokens (immediate requests).
func NewRateLimiter(ratePerMinute, burst int) *RateLimiter {
	rl := &RateLimiter{
		rate:   float64(ratePerMinute) / 60.0,
		burst:  burst,
		stopCh: make(chan struct{}),
	}
	rl.engine = newRateLimiterEngineWithBuckets(&rl.buckets)
	go rl.cleanupLoop()
	return rl
}

// SetEndpointLimits configures per-endpoint rate limits.
// Keys are path prefixes; the longest matching prefix wins.
func (rl *RateLimiter) SetEndpointLimits(limits map[string]EndpointLimit) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.endpointLimits = limits
}

// Stop shuts down the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

func (rl *RateLimiter) SetOnRateLimited(callback func()) {
	rl.onRateLimited = callback
}

// allow checks if a request identified by key is allowed under the given rate and burst.
// Returns allowed=false and the duration to wait before retrying when rate limited.
func (rl *RateLimiter) allow(key string, rate float64, burst int) (bool, time.Duration) {
	return rl.engine.Allow(key, rate, burst)
}

// Middleware returns an HTTP middleware that rate-limits mutating requests (POST/PUT/DELETE)
// based on client IP. Non-mutating requests (GET/HEAD/OPTIONS) pass through unmodified,
// except for explicitly rate-limited expensive read endpoints.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractClientIP(r)

		rl.mu.RLock()
		limits := rl.endpointLimits
		rl.mu.RUnlock()

		decision := NewRateLimitDecisionModule(int(rl.rate*60), rl.burst, limits).Decide(r.Method, r.URL.Path, ip)
		if !decision.Limited {
			next.ServeHTTP(w, r)
			return
		}

		allowed, retryAfter := rl.allow(decision.Key, decision.RatePerSecond, decision.Burst)
		if !allowed {
			if rl.onRateLimited != nil {
				rl.onRateLimited()
			}
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			writeError(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

func (rl *RateLimiter) cleanup() {
	rl.engine.Cleanup(time.Now().Add(-10 * time.Minute))
}
