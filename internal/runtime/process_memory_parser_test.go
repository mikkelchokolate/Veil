package runtime

import "testing"

func TestProcessMemoryParserConvertsRSSPagesToMB(t *testing.T) {
	memory := NewProcessMemoryParser().Parse("1000 512 0 0 0 0 0")
	if memory != 2 {
		t.Fatalf("memory = %d", memory)
	}
}

func TestProcessMemoryParserReturnsZeroForShortInput(t *testing.T) {
	if memory := NewProcessMemoryParser().Parse("1000"); memory != 0 {
		t.Fatalf("memory = %d", memory)
	}
}
