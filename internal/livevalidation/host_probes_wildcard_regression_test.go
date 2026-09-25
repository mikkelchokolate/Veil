package livevalidation

import (
	"context"
	"net"
	"testing"
)

// TestHostPortProbeDetectsNonLoopbackIPv4Bind covers #1031: the old probe
// bound 127.0.0.1 only, so a service listening on a specific non-default
// address (a second loopback IP here) evaded conflict detection while the
// runtime's wildcard bind would still fail at apply.
func TestHostPortProbeDetectsNonLoopbackIPv4Bind(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("127.0.0.2 not bindable on this host: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	available, err := (HostPortProbe{}).Available(context.Background(), "tcp", port)
	if err != nil {
		t.Fatalf("probe TCP: %v", err)
	}
	if available {
		t.Fatalf("TCP port %d bound on 127.0.0.2 reported available for a wildcard bind", port)
	}
}

// TestHostPortProbeDetectsIPv6LoopbackBind covers the IPv6 half of #1031: a
// service bound to ::1 alone blocked the runtime's wildcard bind but not the
// old 127.0.0.1 probe.
func TestHostPortProbeDetectsIPv6LoopbackBind(t *testing.T) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable on this host: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	available, err := (HostPortProbe{}).Available(context.Background(), "tcp", port)
	if err != nil {
		t.Fatalf("probe TCP: %v", err)
	}
	if available {
		t.Fatalf("TCP port %d bound on ::1 reported available for a wildcard bind", port)
	}
}

// TestHostPortProbeDetectsNonLoopbackUDPBind is the UDP twin: a packet
// socket bound to a specific address must also count as occupied.
func TestHostPortProbeDetectsNonLoopbackUDPBind(t *testing.T) {
	packet, err := net.ListenPacket("udp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("127.0.0.2 not bindable on this host: %v", err)
	}
	defer packet.Close()
	port := packet.LocalAddr().(*net.UDPAddr).Port

	available, err := (HostPortProbe{}).Available(context.Background(), "udp", port)
	if err != nil {
		t.Fatalf("probe UDP: %v", err)
	}
	if available {
		t.Fatalf("UDP port %d bound on 127.0.0.2 reported available for a wildcard bind", port)
	}
}
