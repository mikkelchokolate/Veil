package runtime

import "testing"

func TestParseKBExtractsSecondField(t *testing.T) {
	if got := parseKB("MemTotal:       2048000 kB"); got != 2048000 {
		t.Fatalf("parseKB = %d", got)
	}
}

func TestParseKBReturnsZeroForShortInput(t *testing.T) {
	if got := parseKB("MemTotal:"); got != 0 {
		t.Fatalf("parseKB = %d", got)
	}
	if got := parseKB(""); got != 0 {
		t.Fatalf("parseKB = %d", got)
	}
}
