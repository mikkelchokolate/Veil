package observability

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type MetricsRequestRecorder struct {
	collector *MetricsCollector
}

func NewMetricsRequestRecorder(collector *MetricsCollector) MetricsRequestRecorder {
	return MetricsRequestRecorder{collector: collector}
}

func (r MetricsRequestRecorder) Record(method, path string, statusCode int, duration time.Duration) {
	m := r.collector
	m.requestsTotal.Add(1)
	r.increment(&m.requestsByCode, strconv.Itoa(statusCode))
	r.increment(&m.requestsByPath, normalizeHTTPMethodLabel(method)+":"+NormalizeHTTPPath(path))
	m.requestDuration.add(duration)
}

// normalizeHTTPMethodLabel maps the request method onto a bounded label set.
// The raw method string is attacker-controlled (any valid HTTP token is
// accepted by the server), so recording it verbatim would let a caller mint
// a new metric series per arbitrary method — unbounded cardinality in the
// permanent requestsByPath map (#1107). Recognized methods keep their own
// series; every other token collapses into {other}.
func normalizeHTTPMethodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
		return method
	default:
		return "{other}"
	}
}

func (MetricsRequestRecorder) increment(values *sync.Map, key string) {
	var counter atomic.Int64
	counter.Store(1)
	if actual, loaded := values.LoadOrStore(key, &counter); loaded {
		actual.(*atomic.Int64).Add(1)
	}
}
