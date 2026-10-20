package runtimeports

import (
	"sort"
	"testing"
)

func TestACMEHTTP01ParkedAddress(t *testing.T) {
	if got := ACMEHTTP01ParkedAddress(); got != "127.42.0.2:44080" {
		t.Fatalf("ACMEHTTP01ParkedAddress() = %q", got)
	}
}

// #1285: every loopback TCP port a Veil service binds must be listed, and
// the list must be ascending (installer.RandomHighPort relies on the order).
func TestReservedInternalTCPPortsCoversEveryInternalListener(t *testing.T) {
	ports := ReservedInternalTCPPorts()
	if !sort.IntsAreSorted(ports) {
		t.Fatalf("ReservedInternalTCPPorts must be ascending, got %v", ports)
	}
	for _, want := range []int{CaddyAdminPort, ACMEHTTP01ParkedPort, Hysteria2TrafficStatsPort, Hysteria2HTTPAuthPort} {
		if !IsReservedInternalTCPPort(want) {
			t.Fatalf("port %d missing from ReservedInternalTCPPorts %v", want, ports)
		}
	}
	for _, port := range []int{0, 443, 2096, ACMEHTTP01ParkedPort + 1} {
		if IsReservedInternalTCPPort(port) {
			t.Fatalf("port %d must not be reserved", port)
		}
	}
}
