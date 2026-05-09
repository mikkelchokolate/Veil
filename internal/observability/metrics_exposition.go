package observability

import (
	"fmt"
	"sync/atomic"
	"time"
)

type MetricsExposition struct {
	collector *MetricsCollector
}

func NewMetricsExposition(collector *MetricsCollector) MetricsExposition {
	return MetricsExposition{collector: collector}
}

func (e MetricsExposition) Render() string {
	m := e.collector
	var b []byte

	b = append(b, "# HELP veil_uptime_seconds Time since Veil started.\n"...)
	b = append(b, "# TYPE veil_uptime_seconds gauge\n"...)
	b = append(b, fmt.Sprintf("veil_uptime_seconds %.0f\n", time.Since(m.startTime).Seconds())...)

	b = append(b, "# HELP veil_http_requests_total Total HTTP requests.\n"...)
	b = append(b, "# TYPE veil_http_requests_total counter\n"...)
	b = append(b, fmt.Sprintf("veil_http_requests_total %d\n", m.requestsTotal.Load())...)

	b = append(b, "# HELP veil_http_requests_duration_seconds_avg Average request duration.\n"...)
	b = append(b, "# TYPE veil_http_requests_duration_seconds_avg gauge\n"...)
	b = append(b, fmt.Sprintf("veil_http_requests_duration_seconds_avg %f\n", m.requestDuration.average())...)

	b = append(b, "# HELP veil_http_requests_active Currently active requests.\n"...)
	b = append(b, "# TYPE veil_http_requests_active gauge\n"...)
	b = append(b, fmt.Sprintf("veil_http_requests_active %d\n", m.activeRequests.Load())...)

	b = append(b, "# HELP veil_rate_limit_hits_total Rate-limited requests.\n"...)
	b = append(b, "# TYPE veil_rate_limit_hits_total counter\n"...)
	b = append(b, fmt.Sprintf("veil_rate_limit_hits_total %d\n", m.rateLimitHits.Load())...)

	b = append(b, "# HELP veil_http_requests_by_code_total HTTP requests by status code.\n"...)
	b = append(b, "# TYPE veil_http_requests_by_code_total counter\n"...)
	m.requestsByCode.Range(func(key, value any) bool {
		b = append(b, fmt.Sprintf("veil_http_requests_by_code_total{code=\"%s\"} %d\n", PrometheusLabelValue(key.(string)), value.(*atomic.Int64).Load())...)
		return true
	})

	b = append(b, "# HELP veil_http_requests_by_path_total HTTP requests by method and path.\n"...)
	b = append(b, "# TYPE veil_http_requests_by_path_total counter\n"...)
	m.requestsByPath.Range(func(key, value any) bool {
		b = append(b, fmt.Sprintf("veil_http_requests_by_path_total{path=\"%s\"} %d\n", PrometheusLabelValue(key.(string)), value.(*atomic.Int64).Load())...)
		return true
	})

	m.statusMu.RLock()
	if len(m.serviceStatus) > 0 {
		b = append(b, "# HELP veil_service_status Service active status (1=active, 0=inactive).\n"...)
		b = append(b, "# TYPE veil_service_status gauge\n"...)
		for name, val := range m.serviceStatus {
			b = append(b, fmt.Sprintf("veil_service_status{service=\"%s\"} %g\n", PrometheusLabelValue(name), val)...)
		}
	}
	m.statusMu.RUnlock()

	return string(b)
}
