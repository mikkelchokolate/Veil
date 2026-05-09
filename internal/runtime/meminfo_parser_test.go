package runtime

import "testing"

func TestMeminfoParserComputesUsedMemoryFromAvailable(t *testing.T) {
	mem := NewMeminfoParser().Parse(`MemTotal:       2048000 kB
MemFree:         100000 kB
MemAvailable:   1536000 kB
Buffers:          10000 kB
Cached:           20000 kB
`)
	if mem.total != 2048000 || mem.used != 512000 {
		t.Fatalf("mem = %+v", mem)
	}
}

func TestMeminfoParserReturnsZeroWhenTotalMissing(t *testing.T) {
	mem := NewMeminfoParser().Parse(`MemAvailable: 100 kB`)
	if mem != (memInfo{}) {
		t.Fatalf("mem = %+v", mem)
	}
}
