package runtime

import "testing"

func TestConnectionSocketRowParserParsesListeningTCPAndUDPRows(t *testing.T) {
	parser := NewConnectionSocketRowParser()
	row, ok := parser.Parse("tcp", "0: 0100007F:0830 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111")
	if !ok {
		t.Fatal("expected tcp row")
	}
	if row != (ConnectionSocketRow{Proto: "tcp", Address: "127.0.0.1", Port: 2096}) {
		t.Fatalf("tcp row = %+v", row)
	}
	row, ok = parser.Parse("udp", "2: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 333")
	if !ok {
		t.Fatal("expected udp row")
	}
	if row != (ConnectionSocketRow{Proto: "udp", Address: "0.0.0.0", Port: 53}) {
		t.Fatalf("udp row = %+v", row)
	}
}

func TestConnectionSocketRowParserSkipsHeadersMalformedAndNonListeningTCP(t *testing.T) {
	parser := NewConnectionSocketRowParser()
	for _, line := range []string{
		"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
		"malformed",
		"1: 0100007F:0831 00000000:0000 01 00000000:00000000 00:00000000 00000000 0 0 222",
		"1: bad 00000000:0000 0A",
	} {
		if row, ok := parser.Parse("tcp", line); ok {
			t.Fatalf("expected skip for %q, got %+v", line, row)
		}
	}
}
