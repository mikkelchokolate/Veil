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
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
}

// isRateLimitedReadPath returns true for GET paths that should be rate-limited
// (expensive queries like log reading).
func isRateLimitedReadPath(path string) bool {
	return strings.HasPrefix(path, "/api/logs")
}
