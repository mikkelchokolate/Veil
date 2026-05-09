package runtime

import "testing"

func TestProcessStatParserParsesTicksAndStartTimeAfterCommandWithSpaces(t *testing.T) {
	stat := "123 (veil worker) S 0 0 0 0 0 0 0 0 0 0 11 22 0 0 0 0 0 0 3300 0"
	parsed, ok := NewProcessStatParser().Parse(stat)
	if !ok {
		t.Fatal("expected parse success")
	}
	if parsed.UserTicks != 11 || parsed.SystemTicks != 22 || parsed.StartTimeTicks != 3300 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestProcessStatParserRejectsMalformedStat(t *testing.T) {
	if _, ok := NewProcessStatParser().Parse("bad stat"); ok {
		t.Fatal("expected parse failure")
	}
}
