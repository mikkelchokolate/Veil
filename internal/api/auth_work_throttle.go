package api

import (
	"runtime"
	"time"

	"github.com/mikkelchokolate/Veil/internal/observability"
)

// bcryptWorkSlots bounds concurrent password-hash work on unauthenticated
// endpoints. Every login attempt — including ones for unknown usernames —
// pays a full bcrypt compare (#1064), so without a ceiling an
// address-rotating spray could pin every CPU on the proxy host (#1101).
var bcryptWorkSlots = make(chan struct{}, maxBcryptConcurrency())

func maxBcryptConcurrency() int {
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 1
}

// acquireBcryptWork returns a release function, or nil when the global
// bcrypt budget is saturated and the caller must refuse the request.
func acquireBcryptWork() func() {
	select {
	case bcryptWorkSlots <- struct{}{}:
		return func() { <-bcryptWorkSlots }
	default:
		return nil
	}
}

// delayGlobalUsernameAttempt applies a process-wide per-username slowdown
// on top of the per-(client,username) budget. The per-address budgets are
// aggregated to an IPv6 /64 — the largest practical rotation unit — but a
// spray sourced from MANY prefixes still gets one fresh budget per prefix;
// this global budget caps total bcrypt churn against a single username no
// matter how many sources join in (#1101, building on #667). Requests over
// budget are delayed rather than rejected so a legitimate operator is only
// slowed — never locked out — by someone else's spray.
func (s *managementState) delayGlobalUsernameAttempt(username string) {
	s.mu.Lock()
	if s.loginGlobalLimiter == nil {
		s.loginGlobalLimiter = observability.NewBoundedRateLimiterEngine(maxLoginUsernameBuckets)
	}
	limiter := s.loginGlobalLimiter
	s.mu.Unlock()
	allowed, retryAfter := limiter.Allow("login-global:"+loginUsernameKey(username), 30.0/60.0, 10)
	if allowed || retryAfter <= 0 {
		return
	}
	// Cap the sleep: the spray itself still hits the per-IP HTTP limit and
	// the bcrypt semaphore; a slightly longer queue is DoS-safe enough while
	// the honest operator's retry stays tolerable.
	if retryAfter > 2*time.Second {
		retryAfter = 2 * time.Second
	}
	time.Sleep(retryAfter)
}
