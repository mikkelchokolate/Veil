package api

import (
	"net/http"

	"github.com/veil-panel/veil/internal/observability"
)

type MetricsCollector = observability.MetricsCollector
type MetricsDurationAccumulator = observability.MetricsDurationAccumulator
type MetricsExposition = observability.MetricsExposition
type MetricsMiddlewareModule = observability.MetricsMiddlewareModule
type MetricsRequestRecorder = observability.MetricsRequestRecorder
type MetricsServiceStatus = observability.MetricsServiceStatus
type HTTPStatusRecorder = observability.HTTPStatusRecorder
type EndpointLimit = observability.EndpointLimit
type RateLimiter = observability.RateLimiter
type RateLimiterEngine = observability.RateLimiterEngine
type RateLimitDecision = observability.RateLimitDecision
type RateLimitDecisionModule = observability.RateLimitDecisionModule
type RateLimitPolicy = observability.RateLimitPolicy

func NewMetricsCollector() *MetricsCollector { return observability.NewMetricsCollector() }
func NewMetricsDurationAccumulator() *MetricsDurationAccumulator {
	return observability.NewMetricsDurationAccumulator()
}
func NewMetricsExposition(collector *MetricsCollector) MetricsExposition {
	return observability.NewMetricsExposition(collector)
}
func NewMetricsMiddlewareModule(collector *MetricsCollector) MetricsMiddlewareModule {
	return observability.NewMetricsMiddlewareModule(collector)
}
func NewMetricsRequestRecorder(collector *MetricsCollector) MetricsRequestRecorder {
	return observability.NewMetricsRequestRecorder(collector)
}
func NewMetricsServiceStatus(collector *MetricsCollector) MetricsServiceStatus {
	return observability.NewMetricsServiceStatus(collector)
}
func NewHTTPStatusRecorder(w http.ResponseWriter) *HTTPStatusRecorder {
	return observability.NewHTTPStatusRecorder(w)
}
func NewRateLimiter(ratePerMinute, burst int) *RateLimiter {
	return observability.NewRateLimiter(ratePerMinute, burst)
}
func NewRateLimiterEngine() *RateLimiterEngine { return observability.NewRateLimiterEngine() }
func NewRateLimitDecisionModule(defaultRatePerMinute, defaultBurst int, limits map[string]EndpointLimit) RateLimitDecisionModule {
	return observability.NewRateLimitDecisionModule(defaultRatePerMinute, defaultBurst, limits)
}
func DefaultRateLimitPolicy() RateLimitPolicy  { return observability.DefaultRateLimitPolicy() }
func PrometheusLabelValue(value string) string { return observability.PrometheusLabelValue(value) }
