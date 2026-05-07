package api

import (
	"net/http"
	"time"
)

type MetricsMiddlewareModule struct {
	collector *MetricsCollector
}

func NewMetricsMiddlewareModule(collector *MetricsCollector) MetricsMiddlewareModule {
	return MetricsMiddlewareModule{collector: collector}
}

func (m MetricsMiddlewareModule) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		m.collector.activeRequests.Add(1)
		defer m.collector.activeRequests.Add(-1)

		crw := &codeResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(crw, r)

		m.collector.TrackRequest(r.Method, r.URL.Path, crw.statusCode, time.Since(start))
	})
}
