package observability

import (
	"net/http"
	"strings"
)

type RateLimitDecisionModule struct {
	defaultRatePerMinute int
	defaultBurst         int
	limits               map[string]EndpointLimit
}

type RateLimitDecision struct {
	Limited       bool
	Key           string
	RatePerSecond float64
	Burst         int
}

func NewRateLimitDecisionModule(defaultRatePerMinute, defaultBurst int, limits map[string]EndpointLimit) RateLimitDecisionModule {
	return RateLimitDecisionModule{defaultRatePerMinute: defaultRatePerMinute, defaultBurst: defaultBurst, limits: limits}
}

func (m RateLimitDecisionModule) Decide(method, path, ip string) RateLimitDecision {
	if !isMutatingMethod(method) && !isRateLimitedReadPath(path) {
		return RateLimitDecision{}
	}
	if limit, prefix := m.endpointLimit(path); prefix != "" {
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

func (m RateLimitDecisionModule) endpointLimit(path string) (EndpointLimit, string) {
	var bestMatch string
	var bestLimit EndpointLimit
	for prefix, limit := range m.limits {
		if strings.HasPrefix(path, prefix) && len(prefix) > len(bestMatch) {
			bestMatch = prefix
			bestLimit = limit
		}
	}
	return bestLimit, bestMatch
}

func isMutatingMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodDelete || method == http.MethodPatch
}

// isRateLimitedReadPath returns true for GET paths that should be rate-limited
// (expensive queries like log reading, long-lived SSE stream opens, and
// admin-secret credential/export reads that must not be unlimited).
func isRateLimitedReadPath(path string) bool {
	if strings.HasPrefix(path, "/api/logs") ||
		path == "/api/v1/events" ||
		path == "/api/v1/traffic/stream" ||
		strings.HasPrefix(path, "/api/client-links") {
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
