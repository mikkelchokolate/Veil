package observability

import (
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
	r.increment(&m.requestsByPath, method+":"+path)
	m.requestDuration.add(duration)
}

func (MetricsRequestRecorder) increment(values *sync.Map, key string) {
	var counter atomic.Int64
	counter.Store(1)
	if actual, loaded := values.LoadOrStore(key, &counter); loaded {
		actual.(*atomic.Int64).Add(1)
	}
}
