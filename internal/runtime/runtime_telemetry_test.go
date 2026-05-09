package runtime

import (
	"errors"
	"testing"
)

func TestRuntimeTelemetryCollectsSystemAndPropagatesErrors(t *testing.T) {
	telemetry := NewRuntimeTelemetry()
	telemetry.readSystem = func() (SystemStats, error) {
		return SystemStats{MemoryTotalMB: 2048}, nil
	}
	stats, err := telemetry.System()
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	if stats.MemoryTotalMB != 2048 {
		t.Fatalf("stats = %+v", stats)
	}

	telemetry.readSystem = func() (SystemStats, error) {
		return SystemStats{}, errors.New("boom")
	}
	if _, err := telemetry.System(); err == nil {
		t.Fatalf("expected error")
	}
}
