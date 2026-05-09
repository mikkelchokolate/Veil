package runtime

import "testing"

func TestUptimeParserParsesSeconds(t *testing.T) {
	seconds := NewUptimeParser().Parse("12345.67 890.12")
	if seconds != 12345 {
		t.Fatalf("seconds = %d", seconds)
	}
}

func TestUptimeParserReturnsZeroForEmptyInput(t *testing.T) {
	if seconds := NewUptimeParser().Parse(""); seconds != 0 {
		t.Fatalf("seconds = %d", seconds)
	}
}
