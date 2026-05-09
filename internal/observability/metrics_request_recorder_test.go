package observability

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestMetricsRequestRecorderRecordsRequestCountersAndDuration(t *testing.T) {
	collector := NewMetricsCollector()
	NewMetricsRequestRecorder(collector).Record("POST", "/api/settings", 201, 250*time.Millisecond)
	if collector.requestsTotal.Load() != 1 {
		t.Fatalf("requestsTotal = %d", collector.requestsTotal.Load())
	}
	if got, ok := collector.requestsByCode.Load("201"); !ok || got.(*atomic.Int64).Load() != 1 {
		t.Fatalf("requestsByCode[201] = %v ok=%v", got, ok)
	}
	if got, ok := collector.requestsByPath.Load("POST:/api/settings"); !ok || got.(*atomic.Int64).Load() != 1 {
		t.Fatalf("requestsByPath = %v ok=%v", got, ok)
	}
	if avg := collector.requestDuration.average(); avg != 0.25 {
		t.Fatalf("average duration = %f", avg)
	}
}
