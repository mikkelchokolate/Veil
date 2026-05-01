package installer

import (
	"net"
	"testing"
)

func TestDetectPortAvailabilityReportsFreePorts(t *testing.T) {
	port := freeTCPPort(t)
	availability, err := DetectPortAvailability([]int{port})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if availability.TCPBusy[port] {
		t.Fatalf("expected tcp/%d to be free", port)
	}
	if availability.UDPBusy[port] {
		t.Fatalf("expected udp/%d to be free", port)
	}
}

func TestDetectPortAvailabilityReportsBusyTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	availability, err := DetectPortAvailability([]int{port})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !availability.TCPBusy[port] {
		t.Fatalf("expected tcp/%d busy", port)
	}
	if availability.UDPBusy[port] {
		t.Fatalf("expected udp/%d free", port)
	}
}

func TestDetectPortAvailabilityReportsBusyUDP(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	availability, err := DetectPortAvailability([]int{port})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if availability.TCPBusy[port] {
		t.Fatalf("expected tcp/%d free", port)
	}
	if !availability.UDPBusy[port] {
		t.Fatalf("expected udp/%d busy", port)
	}
}

func TestRandomHighPortIsInExpectedRange(t *testing.T) {
	port, err := RandomHighPort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port < 20000 || port > 50000 {
		t.Fatalf("expected port in 20000..50000, got %d", port)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
