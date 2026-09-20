package livevalidation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"strings"
	"syscall"
	"testing"
)

const procNetHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"

func TestProcNetLocalPortParsesIPv4AndIPv6(t *testing.T) {
	port, ok := procNetLocalPort("00000000:01BB")
	if !ok || port != 443 {
		t.Fatalf("ipv4 port = %d ok=%v", port, ok)
	}
	port, ok = procNetLocalPort("00000000000000000000000000000000:01BB")
	if !ok || port != 443 {
		t.Fatalf("ipv6 port = %d ok=%v", port, ok)
	}
	if _, ok := procNetLocalPort("bad"); ok {
		t.Fatal("expected malformed address to fail")
	}
}

func TestProcNetContainsPortCountsUDPBindsAndTCPListenersOnly(t *testing.T) {
	udp := procNetHeader + "\n" +
		"  46: 00000000:01BB 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 1 2 0000000000000000 0\n"
	if !procNetContainsPort([]byte(udp), 443, "udp") {
		t.Fatal("expected UDP 443 bind to count")
	}
	if procNetContainsPort([]byte(udp), 8443, "udp") {
		t.Fatal("UDP table should not report a different port")
	}

	// #584: a connected UDP row (local:443 -> peer:53) is not a listener and
	// must not mark the port busy.
	udpConnected := procNetHeader + "\n" +
		"  47: 00000000:01BB 08080808:0035 07 00000000:00000000 00:00000000 00000000     0        0 1 2 0000000000000000 0\n"
	if procNetContainsPort([]byte(udpConnected), 443, "udp") {
		t.Fatal("connected UDP socket must not count as occupying the port")
	}
	udp6Bound := procNetHeader + "\n" +
		"  48: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 1 2 0000000000000000 0\n"
	if !procNetContainsPort([]byte(udp6Bound), 443, "udp") {
		t.Fatal("expected wildcard-remote UDP6 443 bind to count")
	}
	udp6Connected := procNetHeader + "\n" +
		"  49: 00000000000000000000000000000000:01BB 26060000000000000000000000008888:0035 07 00000000:00000000 00:00000000 00000000     0        0 1 2 0000000000000000 0\n"
	if procNetContainsPort([]byte(udp6Connected), 443, "udp") {
		t.Fatal("connected UDP6 socket must not count as occupying the port")
	}

	tcpEstablished := procNetHeader + "\n" +
		"   1: 0000000000000000FFFF000036E99D2D:01BB 0000000000000000FFFF00006812E82F:A0C8 01 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 0\n"
	if procNetContainsPort([]byte(tcpEstablished), 443, "tcp") {
		t.Fatal("TCP ESTABLISHED should not count as occupying the listen port")
	}

	tcpListen := procNetHeader + "\n" +
		"   2: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 0\n"
	if !procNetContainsPort([]byte(tcpListen), 443, "tcp") {
		t.Fatal("expected TCP LISTEN 443 to count")
	}
}

// #584: on the EACCES privileged-port fallback a connected (non-listen) UDP
// row for the candidate port must not report it busy — only a wildcard-remote
// bound socket occupies a UDP port.
func TestHostPortProbeTreatsConnectedUDPRowAsAvailableOnPermissionDenied(t *testing.T) {
	probe := HostPortProbe{
		listenUDP: func(context.Context, *net.ListenConfig, string) (net.PacketConn, error) {
			return nil, syscall.EACCES
		},
		readProcNet: func(name string) ([]byte, error) {
			if name == "udp" {
				return []byte(procNetHeader + "\n  1: 00000000:01BB 08080808:0035 07 00000000:00000000 00:00000000 00000000 0 0 1 2 0 0\n"), nil
			}
			return []byte(procNetHeader + "\n"), nil
		},
	}

	available, err := probe.Available(context.Background(), "udp", 443)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if !available {
		t.Fatal("connected (non-listen) UDP socket on 443 reported busy")
	}
}

func TestHostPortProbeTreatsPermissionDeniedOnFreePrivilegedUDPPortAsAvailable(t *testing.T) {
	probe := HostPortProbe{
		listenUDP: func(context.Context, *net.ListenConfig, string) (net.PacketConn, error) {
			return nil, syscall.EACCES
		},
		readProcNet: func(name string) ([]byte, error) {
			if name != "udp" && name != "udp6" {
				t.Fatalf("unexpected table %q", name)
			}
			return []byte(procNetHeader + "\n"), nil
		},
	}

	available, err := probe.Available(context.Background(), "udp", 443)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if !available {
		t.Fatal("free UDP 443 reported busy after EACCES")
	}
}

func TestHostPortProbeTreatsPermissionDeniedOnBoundPrivilegedUDPPortAsBusy(t *testing.T) {
	probe := HostPortProbe{
		listenUDP: func(context.Context, *net.ListenConfig, string) (net.PacketConn, error) {
			return nil, syscall.EACCES
		},
		readProcNet: func(name string) ([]byte, error) {
			if name == "udp" {
				return []byte(procNetHeader + "\n  1: 00000000:01BB 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 1 2 0 0\n"), nil
			}
			return []byte(procNetHeader + "\n"), nil
		},
	}

	available, err := probe.Available(context.Background(), "udp", 443)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if available {
		t.Fatal("bound UDP 443 reported available")
	}
}

func TestHostPortProbeTreatsPermissionDeniedOnTCPListenAsBusy(t *testing.T) {
	probe := HostPortProbe{
		listenTCP: func(context.Context, *net.ListenConfig, string) (net.Listener, error) {
			return nil, syscall.EACCES
		},
		readProcNet: func(name string) ([]byte, error) {
			if name == "tcp6" {
				return []byte(procNetHeader + "\n   2: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1 1 0 0\n"), nil
			}
			return []byte(procNetHeader + "\n"), nil
		},
	}

	available, err := probe.Available(context.Background(), "tcp", 443)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if available {
		t.Fatal("TCP LISTEN 443 reported available")
	}
}

func TestHostPortProbeDoesNotConsultProcNetOnAddressInUse(t *testing.T) {
	probe := HostPortProbe{
		listenUDP: func(context.Context, *net.ListenConfig, string) (net.PacketConn, error) {
			return nil, syscall.EADDRINUSE
		},
		readProcNet: func(string) ([]byte, error) {
			t.Fatal("proc net should not be read for EADDRINUSE")
			return nil, errors.New("unused")
		},
	}

	available, err := probe.Available(context.Background(), "udp", 443)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if available {
		t.Fatal("EADDRINUSE reported available")
	}
}

func TestHostPortProbeReportsErrorWhenProcNetUnavailableAfterPermissionDenied(t *testing.T) {
	probe := HostPortProbe{
		listenUDP: func(context.Context, *net.ListenConfig, string) (net.PacketConn, error) {
			return nil, syscall.EACCES
		},
		readProcNet: func(name string) ([]byte, error) {
			return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
		},
	}

	_, err := probe.Available(context.Background(), "udp", 443)
	if err == nil || !strings.Contains(err.Error(), "/proc/net") {
		t.Fatalf("error = %v", err)
	}
}
