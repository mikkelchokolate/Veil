package runtime

import "testing"

func TestLoadAvgParserParsesFirstThreeAverages(t *testing.T) {
	load := NewLoadAvgParser().Parse("1.23 2.34 3.45 1/234 5678")
	if load.avg1 != 1.23 || load.avg5 != 2.34 || load.avg15 != 3.45 {
		t.Fatalf("load = %+v", load)
	}
}

func TestLoadAvgParserReturnsZeroForShortInput(t *testing.T) {
	if load := NewLoadAvgParser().Parse("1.23"); load != (loadAvg{}) {
		t.Fatalf("load = %+v", load)
	}
}
