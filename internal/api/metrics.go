// Package metrics provides Prometheus-compatible metrics exposition for Veil.
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// MetricsCollector tracks HTTP and application metrics in Prometheus-compatible format.
type MetricsCollector struct {
	// HTTP metrics
	requestsTotal   atomic.Int64
	requestsByCode  sync.Map // map[int]*atomic.Int64
	requestsByPath  sync.Map // map[string]*atomic.Int64
	activeRequests  atomic.Int64
	requestDuration cumulativeDuration

	// Rate limit metrics
	rateLimitHits atomic.Int64

	// Service status — set externally
	serviceStatus map[string]float64 // 1=active, 0=inactive
	statusMu      sync.RWMutex

	startTime time.Time
}

type cumulativeDuration struct {
	mu    sync.Mutex
	total time.Duration
	count int64
}

func (d *cumulativeDuration) add(dur time.Duration) {
	d.mu.Lock()
	d.total += dur
	d.count++
	d.mu.Unlock()
}

func (d *cumulativeDuration) average() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.count == 0 {
		return 0
	}
	return d.total.Seconds() / float64(d.count)
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime: time.Now(),
	}
}

// TrackRequest records an HTTP request for metrics.
func (m *MetricsCollector) TrackRequest(method, path string, statusCode int, duration time.Duration) {
	m.requestsTotal.Add(1)

	// Status code counter
	codeKey := strconv.Itoa(statusCode)
	if val, ok := m.requestsByCode.Load(codeKey); ok {
		val.(*atomic.Int64).Add(1)
	} else {
		var counter atomic.Int64
		counter.Store(1)
		m.requestsByCode.Store(codeKey, &counter)
	}

	// Path counter (method:path)
	pathKey := method + ":" + path
	if val, ok := m.requestsByPath.Load(pathKey); ok {
		val.(*atomic.Int64).Add(1)
	} else {
		var counter atomic.Int64
		counter.Store(1)
		m.requestsByPath.Store(pathKey, &counter)
	}

	m.requestDuration.add(duration)
}

// TrackRateLimitHit records a rate-limited request.
func (m *MetricsCollector) TrackRateLimitHit() {
	m.rateLimitHits.Add(1)
}

// SetServiceStatus updates the gauge for a managed service (1=active, 0=inactive).
func (m *MetricsCollector) SetServiceStatus(name string, active bool) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	if m.serviceStatus == nil {
		m.serviceStatus = make(map[string]float64)
	}
	if active {
		m.serviceStatus[name] = 1
	} else {
		m.serviceStatus[name] = 0
	}
}

// ActiveRequests returns the current active request count (for use with TrackActiveRequest).
func (m *MetricsCollector) ActiveRequests() *atomic.Int64 {
	return &m.activeRequests
}

// ServeHTTP writes Prometheus-compatible metrics to the response.
func (m *MetricsCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if r.Method == http.MethodHead {
		return
	}

	var b []byte

	// HELP/TYPE + metrics
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

	// Per-status-code counters
	b = append(b, "# HELP veil_http_requests_by_code_total HTTP requests by status code.\n"...)
	b = append(b, "# TYPE veil_http_requests_by_code_total counter\n"...)
	m.requestsByCode.Range(func(key, value any) bool {
		b = append(b, fmt.Sprintf("veil_http_requests_by_code_total{code=\"%s\"} %d\n", key.(string), value.(*atomic.Int64).Load())...)
		return true
	})

	// Per-path counters (top paths)
	b = append(b, "# HELP veil_http_requests_by_path_total HTTP requests by method and path.\n"...)
	b = append(b, "# TYPE veil_http_requests_by_path_total counter\n"...)
	m.requestsByPath.Range(func(key, value any) bool {
		b = append(b, fmt.Sprintf("veil_http_requests_by_path_total{path=\"%s\"} %d\n", key.(string), value.(*atomic.Int64).Load())...)
		return true
	})

	// Service status gauges
	m.statusMu.RLock()
	if len(m.serviceStatus) > 0 {
		b = append(b, "# HELP veil_service_status Service active status (1=active, 0=inactive).\n"...)
		b = append(b, "# TYPE veil_service_status gauge\n"...)
		for name, val := range m.serviceStatus {
			b = append(b, fmt.Sprintf("veil_service_status{service=\"%s\"} %g\n", name, val)...)
		}
	}
	m.statusMu.RUnlock()

	w.Write(b)
}

// MetricsMiddleware wraps an http.Handler and tracks request metrics.
func (m *MetricsCollector) MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		m.activeRequests.Add(1)
		defer m.activeRequests.Add(-1)

		// Wrap ResponseWriter to capture status code
		crw := &codeResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(crw, r)

		m.TrackRequest(r.Method, r.URL.Path, crw.statusCode, time.Since(start))
	})
}

type codeResponseWriter struct {
	http.ResponseWriter
	statusCode int
	wroteHeader bool
}

func (w *codeResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *codeResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
