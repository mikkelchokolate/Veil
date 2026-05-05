package api

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
	rate           float64
	burst          int
	endpointLimits map[string]EndpointLimit
	mu             sync.RWMutex
	stopCh         chan struct{}
	onRateLimited  func() // called when a request is rate-limited
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	mu       sync.Mutex
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

// allow checks if a request identified by key is allowed under the given rate and burst.
// Returns allowed=false and the duration to wait before retrying when rate limited.
func (rl *RateLimiter) allow(key string, rate float64, burst int) (bool, time.Duration) {
	val, _ := rl.buckets.LoadOrStore(key, &tokenBucket{
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

	// Calculate time until the next token arrives
	needed := 1.0 - tb.tokens
	retryAfter := time.Duration(needed / rate * float64(time.Second))
	return false, retryAfter
}

// Middleware returns an HTTP middleware that rate-limits mutating requests (POST/PUT/DELETE)
// based on client IP. Non-mutating requests (GET/HEAD/OPTIONS) pass through unmodified.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutatingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		ip := extractClientIP(r)

		rl.mu.RLock()
		limits := rl.endpointLimits
		rl.mu.RUnlock()

		// Find the longest matching endpoint prefix
		if limits != nil {
			var bestMatch string
			var bestLimit EndpointLimit
			for prefix, limit := range limits {
				if strings.HasPrefix(r.URL.Path, prefix) && len(prefix) > len(bestMatch) {
					bestMatch = prefix
					bestLimit = limit
				}
			}
			if bestMatch != "" {
				rate := float64(bestLimit.RatePerMinute) / 60.0
				key := bestMatch + ":" + ip
				allowed, retryAfter := rl.allow(key, rate, bestLimit.Burst)
				if !allowed {
					if rl.onRateLimited != nil {
						rl.onRateLimited()
					}
					w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
					writeError(w, "too many requests", http.StatusTooManyRequests)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		// Default rate limit
		allowed, retryAfter := rl.allow(ip, rl.rate, rl.burst)
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

func isMutatingMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
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
	cutoff := time.Now().Add(-10 * time.Minute)
	rl.buckets.Range(func(key, value any) bool {
		tb := value.(*tokenBucket)
		tb.mu.Lock()
		lastTime := tb.lastTime
		tb.mu.Unlock()
		if lastTime.Before(cutoff) {
			rl.buckets.Delete(key)
		}
		return true
	})
}
