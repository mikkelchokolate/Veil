package runtime

import (
	"errors"
	"testing"
)

// Issue #208: IPv6 and dual-stack listeners live in /proc/net/tcp6 and
// /proc/net/udp6 and carry 32-hex-digit local addresses. Discovery must read
// all four proc tables and decode IPv6 rows.

func TestConnectionHexAddressParserParsesIPv6Addresses(t *testing.T) {
	parser := NewConnectionHexAddressParser()
	for _, tc := range []struct {
		value string
		addr  string
		port  int
	}{
		{"00000000000000000000000000000000:01BB", "::", 443},
		{"00000000000000000000000001000000:0035", "::1", 53},
		{"000080FE" + "00000000" + "00000000" + "00000000" + ":01BB", "fe80::", 443},
		{"60480120000060480000000088880000:0050", "2001:4860:4860::8888", 80},
	} {
		addr, port, ok := parser.Parse(tc.value)
		if !ok {
			t.Fatalf("Parse(%q) not ok", tc.value)
		}
		if addr != tc.addr || port != tc.port {
			t.Fatalf("Parse(%q) = %q %d, want %q %d", tc.value, addr, port, tc.addr, tc.port)
		}
	}
}

func TestConnectionSocketRowParserParsesTCP6AndUDP6Rows(t *testing.T) {
	parser := NewConnectionSocketRowParser()
	row, ok := parser.Parse("tcp6", "0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111")
	if !ok {
		t.Fatal("expected tcp6 row")
	}
	if row != (ConnectionSocketRow{Proto: "tcp6", Address: "::", Port: 443}) {
		t.Fatalf("tcp6 row = %+v", row)
	}
	row, ok = parser.Parse("udp6", "1: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 222")
	if !ok {
		t.Fatal("expected udp6 row")
	}
	if row != (ConnectionSocketRow{Proto: "udp6", Address: "::", Port: 443}) {
		t.Fatalf("udp6 row = %+v", row)
	}
	if _, ok = parser.Parse("tcp6", "1: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 01 00000000:00000000 00:00000000 00000000 0 0 222"); ok {
		t.Fatal("non-listening tcp6 row was accepted")
	}
}

type scriptedConnectionSource struct {
	lines map[string][]string
	err   error
}

func (s scriptedConnectionSource) SocketLines(proto string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	lines, ok := s.lines[proto]
	if !ok {
		return nil, errors.New("no such proc table")
	}
	return lines, nil
}

func (scriptedConnectionSource) ProcessByPort(proto string, port int) string { return "" }

func TestConnectionDiscoveryReadsAllProcSocketTables(t *testing.T) {
	source := scriptedConnectionSource{lines: map[string][]string{
		"tcp": {
			"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
			"0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111",
		},
		"udp6": {
			"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
			"0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 222",
		},
	}}
	stats, err := newConnectionDiscoveryWithSource(source).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stats.Listeners) != 2 {
		t.Fatalf("listeners = %+v", stats.Listeners)
	}
	var tcpListener, udp6Listener *ConnectionListener
	for i := range stats.Listeners {
		switch stats.Listeners[i].Proto {
		case "tcp":
			tcpListener = &stats.Listeners[i]
		case "udp6":
			udp6Listener = &stats.Listeners[i]
		}
	}
	if tcpListener == nil || tcpListener.Address != "127.0.0.1" || tcpListener.Port != 8080 {
		t.Fatalf("tcp listener missing: %+v", stats.Listeners)
	}
	if udp6Listener == nil || udp6Listener.Address != "::" || udp6Listener.Port != 443 {
		t.Fatalf("udp6 dual-stack listener missing: %+v", stats.Listeners)
	}
}

func TestConnectionDiscoverySkipsMissingIPv6Tables(t *testing.T) {
	source := scriptedConnectionSource{lines: map[string][]string{
		"udp": {
			"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
			"0: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 333",
		},
	}}
	stats, err := newConnectionDiscoveryWithSource(source).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stats.Listeners) != 1 || stats.Listeners[0].Proto != "udp" || stats.Listeners[0].Port != 53 {
		t.Fatalf("listeners = %+v", stats.Listeners)
	}
}
