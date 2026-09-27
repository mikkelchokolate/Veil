package observability

import (
	"fmt"
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

// The method half of the requestsByPath label must be a bounded set: net/http
// accepts any token-string method, so recording it verbatim let an
// unauthenticated caller mint a permanent metric series per arbitrary method
// (#1107). Spraying many unique methods must collapse into "{other}" while
// recognized methods keep their own series.
func TestMetricsRequestRecorderBoundsMethodLabelCardinality(t *testing.T) {
	collector := NewMetricsCollector()
	recorder := NewMetricsRequestRecorder(collector)

	const sprayed = 10000
	for i := 0; i < sprayed; i++ {
		recorder.Record(fmt.Sprintf("X-BOGUS-%d", i), "/api/status", 401, time.Millisecond)
	}
	recorder.Record("PUT", "/api/status", 200, time.Millisecond)
	recorder.Record("PATCH", "/api/status", 200, time.Millisecond)

	keys := map[string]int64{}
	collector.requestsByPath.Range(func(key, value any) bool {
		keys[key.(string)] = value.(*atomic.Int64).Load()
		return true
	})
	if got := keys["{other}:/api/status"]; got != sprayed {
		t.Fatalf("{other}:/api/status = %d, want %d — sprayed methods must collapse into one series", got, sprayed)
	}
	if got := keys["PUT:/api/status"]; got != 1 {
		t.Fatalf("PUT:/api/status = %d, want 1 — recognized methods keep their own series", got)
	}
	if got := keys["PATCH:/api/status"]; got != 1 {
		t.Fatalf("PATCH:/api/status = %d, want 1 — recognized methods keep their own series", got)
	}
	// 1 "{other}" series + PUT + PATCH — never one entry per sprayed method.
	if len(keys) != 3 {
		t.Fatalf("requestsByPath holds %d series after spraying %d methods, want 3: %v", len(keys), sprayed, keys)
	}
}
