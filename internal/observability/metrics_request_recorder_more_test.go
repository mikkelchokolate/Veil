package observability

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMetricsRequestRecorderIncrementsExistingKeys(t *testing.T) {
	collector := NewMetricsCollector()
	recorder := NewMetricsRequestRecorder(collector)

	recorder.Record("GET", "/api/status", 200, 10*time.Millisecond)
	recorder.Record("GET", "/api/status", 200, 20*time.Millisecond)

	if collector.requestsTotal.Load() != 2 {
		t.Fatalf("requestsTotal = %d, want 2", collector.requestsTotal.Load())
	}
	if got, ok := collector.requestsByCode.Load("200"); !ok || got.(*atomic.Int64).Load() != 2 {
		t.Fatalf("requestsByCode[200] = %v ok=%v, want 2", got, ok)
	}
	if got, ok := collector.requestsByPath.Load("GET:/api/status"); !ok || got.(*atomic.Int64).Load() != 2 {
		t.Fatalf("requestsByPath = %v ok=%v, want 2", got, ok)
	}
}

func TestMetricsRequestRecorderConcurrentRecordsAreAccurate(t *testing.T) {
	collector := NewMetricsCollector()
	recorder := NewMetricsRequestRecorder(collector)

	const goroutines = 200
	const records = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < records; j++ {
				recorder.Record("POST", "/api/settings", 201, time.Millisecond)
			}
		}()
	}
	wg.Wait()

	wantTotal := int64(goroutines * records)
	if collector.requestsTotal.Load() != wantTotal {
		t.Fatalf("requestsTotal = %d, want %d", collector.requestsTotal.Load(), wantTotal)
	}
	if got, ok := collector.requestsByCode.Load("201"); !ok || got.(*atomic.Int64).Load() != wantTotal {
		t.Fatalf("requestsByCode[201] = %v ok=%v, want %d", got, ok, wantTotal)
	}
	if got, ok := collector.requestsByPath.Load("POST:/api/settings"); !ok || got.(*atomic.Int64).Load() != wantTotal {
		t.Fatalf("requestsByPath = %v ok=%v, want %d", got, ok, wantTotal)
	}
}
