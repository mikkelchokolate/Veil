package observability

type RateLimitPolicy struct {
	DefaultRatePerMinute int
	DefaultBurst         int
	limits               map[string]EndpointLimit
}

func DefaultRateLimitPolicy() RateLimitPolicy {
	return RateLimitPolicy{
		DefaultRatePerMinute: 100,
		DefaultBurst:         20,
		limits: map[string]EndpointLimit{
			"/api/tools/speedtest":  {RatePerMinute: 2, Burst: 1},  // 1 req/30s
			"/api/tools/dns-lookup": {RatePerMinute: 10, Burst: 3}, // 10 req/min for DNS lookups
			"/api/tools/ping":       {RatePerMinute: 5, Burst: 2},  // 5 req/min for ping
			"/api/logs":             {RatePerMinute: 10, Burst: 3}, // 10 req/min for log reads
		},
	}
}

func (p RateLimitPolicy) EndpointLimits() map[string]EndpointLimit {
	limits := make(map[string]EndpointLimit, len(p.limits))
	for path, limit := range p.limits {
		limits[path] = limit
	}
	return limits
}

func (p RateLimitPolicy) NewLimiter() *RateLimiter {
	limiter := NewRateLimiter(p.DefaultRatePerMinute, p.DefaultBurst)
	limiter.SetEndpointLimits(p.EndpointLimits())
	return limiter
}
