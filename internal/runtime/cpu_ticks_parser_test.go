package runtime

import "testing"

func TestCPUTicksParserParsesAggregateCPULine(t *testing.T) {
	idle, total := NewCPUTicksParser().Parse("cpu  1 2 3 4 5 6\ncpu0 1 2 3 4")
	if idle != 4 || total != 21 {
		t.Fatalf("idle=%d total=%d", idle, total)
	}
}

func TestCPUTicksParserReturnsZeroWhenAggregateLineMissing(t *testing.T) {
	idle, total := NewCPUTicksParser().Parse("cpu0 1 2 3 4")
	if idle != 0 || total != 0 {
		t.Fatalf("idle=%d total=%d", idle, total)
	}
}
