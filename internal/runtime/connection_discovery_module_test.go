package runtime

import (
	"strconv"
	"testing"
)

func TestConnectionDiscoveryReadsTCPAndUDPListenersFromSource(t *testing.T) {
	source := fakeConnectionSource{
		tables: map[string][]string{
			"tcp": {
				"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
				"0: 0100007F:0830 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111",
				"1: 0100007F:0831 00000000:0000 01 00000000:00000000 00:00000000 00000000 0 0 222",
			},
			"udp": {
				"sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode",
				"2: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 333",
			},
		},
		processes: map[string]string{"tcp:2096": "veil", "udp:53": "caddy"},
	}
	stats, err := newConnectionDiscoveryWithSource(source).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stats.Listeners) != 2 {
		t.Fatalf("listeners = %+v", stats.Listeners)
	}
	if stats.Listeners[0] != (ConnectionListener{Proto: "tcp", Address: "127.0.0.1", Port: 2096, Process: "veil"}) {
		t.Fatalf("tcp listener = %+v", stats.Listeners[0])
	}
	if stats.Listeners[1] != (ConnectionListener{Proto: "udp", Address: "0.0.0.0", Port: 53, Process: "caddy"}) {
		t.Fatalf("udp listener = %+v", stats.Listeners[1])
	}
}

type fakeConnectionSource struct {
	tables    map[string][]string
	processes map[string]string
}

func (s fakeConnectionSource) SocketLines(proto string) ([]string, error) {
	return s.tables[proto], nil
}

func (s fakeConnectionSource) ProcessByPort(proto string, port int) string {
	return s.processes[proto+":"+strconv.Itoa(port)]
}
