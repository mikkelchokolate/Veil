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

// isRateLimitedReadPath returns true for GET/HEAD paths that should be
// rate-limited (expensive queries like log reading, long-lived SSE stream
// opens, public subscription fetches, and admin-secret credential/export
// reads that must not be unlimited). Keep this in sync with the
// DefaultRateLimitPolicy endpoint limits (#337).
func isRateLimitedReadPath(path string) bool {
	if strings.HasPrefix(path, "/api/logs") ||
		path == "/api/v1/events" ||
		path == "/api/v1/traffic/stream" ||
		strings.HasPrefix(path, "/api/client-links") ||
		strings.HasPrefix(path, "/s/") {
		return true
	}
	// Per-resource credential reads: link bundles and token-by-id GETs return
	// admin-secret material and need the same throttle as /api/logs.
	if strings.HasPrefix(path, "/api/v1/clients/") {
		return strings.HasSuffix(path, "/links") || strings.Contains(path, "/tokens/")
	}
	// Backup downloads export the full encrypted state archive.
	if strings.HasPrefix(path, "/api/backups/") {
		return strings.HasSuffix(path, "/download")
	}
	return false
}
