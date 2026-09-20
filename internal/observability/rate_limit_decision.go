package observability

import (
	"net/http"
	"strings"
)

type RateLimitDecisionModule struct {
	defaultRatePerMinute int
	defaultBurst         int
	limits               map[string]EndpointLimit
	// readLimits apply to GET/HEAD only. Prefixes like /api/v1/clients host
	// both reads and mutations; putting their read budget here keeps PATCH /
	// POST / DELETE on the shared mutation budget instead of the tighter
	// read one.
	readLimits map[string]EndpointLimit
}

type RateLimitDecision struct {
	Limited       bool
	Key           string
	RatePerSecond float64
	Burst         int
}

func NewRateLimitDecisionModule(defaultRatePerMinute, defaultBurst int, limits, readLimits map[string]EndpointLimit) RateLimitDecisionModule {
	return RateLimitDecisionModule{defaultRatePerMinute: defaultRatePerMinute, defaultBurst: defaultBurst, limits: limits, readLimits: readLimits}
}

func (m RateLimitDecisionModule) Decide(method, path, ip string) RateLimitDecision {
	mutating := isMutatingMethod(method)
	if !mutating && !isRateLimitedReadPath(path) {
		return RateLimitDecision{}
	}
	if limit, prefix := m.endpointLimit(path, mutating); prefix != "" {
		return RateLimitDecision{
			Limited:       true,
			Key:           prefix + ":" + ip,
			RatePerSecond: float64(limit.RatePerMinute) / 60.0,
			Burst:         limit.Burst,
		}
	}
	return RateLimitDecision{
		Limited:       true,
		Key:           ip,
		RatePerSecond: float64(m.defaultRatePerMinute) / 60.0,
		Burst:         m.defaultBurst,
	}
}

func (m RateLimitDecisionModule) endpointLimit(path string, mutating bool) (EndpointLimit, string) {
	var bestMatch string
	var bestLimit EndpointLimit
	consider := func(limits map[string]EndpointLimit) {
		for prefix, limit := range limits {
			if strings.HasPrefix(path, prefix) && len(prefix) > len(bestMatch) {
				bestMatch = prefix
				bestLimit = limit
			}
		}
	}
	consider(m.limits)
	if !mutating {
		consider(m.readLimits)
	}
	return bestLimit, bestMatch
}

func isMutatingMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodDelete || method == http.MethodPatch
}

// isRateLimitedReadPath returns true for GET/HEAD paths that must be
// rate-limited: expensive queries (log reads), long-lived SSE stream opens,
// public subscription fetches, and credential/export reads that must not be
// scraped unboundedly with an admin secret (#337/#583/#594). Keep this in
// sync with the DefaultRateLimitPolicy endpoint limits.
func isRateLimitedReadPath(path string) bool {
	return strings.HasPrefix(path, "/api/logs") ||
		strings.HasPrefix(path, "/s/") ||
		strings.HasPrefix(path, "/api/client-links") ||
		strings.HasPrefix(path, "/api/v1/clients") ||
		strings.HasPrefix(path, "/api/backups") ||
		path == "/api/v1/events" ||
		path == "/api/v1/traffic/stream"
}
