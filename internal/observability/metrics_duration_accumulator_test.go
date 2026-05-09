package observability

import (
	"testing"
	"time"
)

func TestMetricsDurationAccumulatorComputesAverageSeconds(t *testing.T) {
	acc := NewMetricsDurationAccumulator()
	if acc.AverageSeconds() != 0 {
		t.Fatalf("empty average = %f", acc.AverageSeconds())
	}
	acc.Add(100 * time.Millisecond)
	acc.Add(300 * time.Millisecond)
	if got := acc.AverageSeconds(); got != 0.2 {
		t.Fatalf("average = %f", got)
	}
}
