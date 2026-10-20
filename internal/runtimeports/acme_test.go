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

// #1309: every reserved port names its owning service so install-time
// rejections can say which Veil listener the panel would collide with.
func TestReservedInternalTCPPortOwner(t *testing.T) {
	want := map[int]string{
		CaddyAdminPort:            "the Caddy admin listener",
		ACMEHTTP01ParkedPort:      "the IP-certificate HTTP-01 listener",
		Hysteria2TrafficStatsPort: "Hysteria2 traffic statistics",
		Hysteria2HTTPAuthPort:     "the Hysteria2 auth callback",
	}
	ports := ReservedInternalTCPPorts()
	if len(ports) != len(want) {
		t.Fatalf("ReservedInternalTCPPorts = %v, want exactly the %d ports in %v", ports, len(want), want)
	}
	for _, port := range ports {
		owner, ok := ReservedInternalTCPPortOwner(port)
		if !ok || owner != want[port] {
			t.Fatalf("ReservedInternalTCPPortOwner(%d) = %q, %v; want %q, true", port, owner, ok, want[port])
		}
	}
	for _, port := range []int{0, 443, 2096, ACMEHTTP01ParkedPort + 1} {
		if owner, ok := ReservedInternalTCPPortOwner(port); ok || owner != "" {
			t.Fatalf("ReservedInternalTCPPortOwner(%d) = %q, %v; want \"\", false", port, owner, ok)
		}
	}
}
