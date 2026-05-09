package observability

import (
	"sync"
	"time"
)

type MetricsDurationAccumulator struct {
	mu    sync.Mutex
	total time.Duration
	count int64
}

func NewMetricsDurationAccumulator() *MetricsDurationAccumulator {
	return &MetricsDurationAccumulator{}
}

func (d *MetricsDurationAccumulator) Add(dur time.Duration) {
	d.mu.Lock()
	d.total += dur
	d.count++
	d.mu.Unlock()
}

func (d *MetricsDurationAccumulator) AverageSeconds() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.count == 0 {
		return 0
	}
	return d.total.Seconds() / float64(d.count)
}

// Compatibility wrappers keep existing internal callers stable while the deeper Module owns behavior.
func (d *MetricsDurationAccumulator) add(dur time.Duration) { d.Add(dur) }

func (d *MetricsDurationAccumulator) average() float64 { return d.AverageSeconds() }
