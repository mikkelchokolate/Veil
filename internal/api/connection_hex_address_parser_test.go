package api

import "testing"

func TestConnectionHexAddressParserParsesLittleEndianIPv4AndPort(t *testing.T) {
	addr, port, ok := NewConnectionHexAddressParser().Parse("0100007F:0830")
	if !ok {
		t.Fatal("expected parsed address")
	}
	if addr != "127.0.0.1" || port != 2096 {
		t.Fatalf("addr=%q port=%d", addr, port)
	}
}

func TestConnectionHexAddressParserRejectsMalformedValues(t *testing.T) {
	parser := NewConnectionHexAddressParser()
	for _, value := range []string{"", "bad", "0100007F", "zzzzzzzz:0830", "0100007F:zzzz"} {
		if addr, port, ok := parser.Parse(value); ok || addr != "" || port != 0 {
			t.Fatalf("Parse(%q) = %q %d %v", value, addr, port, ok)
		}
	}
}
