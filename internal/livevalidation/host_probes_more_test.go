package livevalidation

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

type fakeListener struct{}

func (fakeListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (fakeListener) Close() error              { return errors.New("close listener failed") }
func (fakeListener) Addr() net.Addr            { return &net.TCPAddr{} }

type fakePacketConn struct{}

func (fakePacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, errors.New("not implemented")
}
func (fakePacketConn) WriteTo([]byte, net.Addr) (int, error) { return 0, errors.New("not implemented") }
func (fakePacketConn) Close() error                          { return errors.New("close packet failed") }
func (fakePacketConn) LocalAddr() net.Addr                   { return &net.UDPAddr{} }
func (fakePacketConn) SetDeadline(time.Time) error           { return nil }
func (fakePacketConn) SetReadDeadline(time.Time) error       { return nil }
func (fakePacketConn) SetWriteDeadline(time.Time) error      { return nil }

func TestHostPortProbeReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (HostPortProbe{}).Available(ctx, "tcp", 443)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestHostPortProbeReturnsContextErrorWhenListenFailsDuringCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	probe := HostPortProbe{
		listenTCP: func(ctx context.Context, _ *net.ListenConfig, _ string) (net.Listener, error) {
			cancel()
			<-ctx.Done()
			return nil, errors.New("listen aborted")
		},
	}

	_, err := probe.Available(ctx, "tcp", 443)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestHostPortProbeReturnsErrorWhenTCPListenerCloseFails(t *testing.T) {
	probe := HostPortProbe{
		listenTCP: func(context.Context, *net.ListenConfig, string) (net.Listener, error) {
			return fakeListener{}, nil
		},
	}

	_, err := probe.Available(context.Background(), "tcp", 443)
	if err == nil || !strings.Contains(err.Error(), "close listener failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestHostPortProbeReturnsErrorWhenUDPPacketCloseFails(t *testing.T) {
	probe := HostPortProbe{
		listenUDP: func(context.Context, *net.ListenConfig, string) (net.PacketConn, error) {
			return fakePacketConn{}, nil
		},
	}

	_, err := probe.Available(context.Background(), "udp", 443)
	if err == nil || !strings.Contains(err.Error(), "close packet failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestHostPortProbeReturnsContextErrorWhenUDPListenFailsDuringCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	probe := HostPortProbe{
		listenUDP: func(ctx context.Context, _ *net.ListenConfig, _ string) (net.PacketConn, error) {
			cancel()
			<-ctx.Done()
			return nil, errors.New("listen aborted")
		},
	}

	_, err := probe.Available(ctx, "udp", 443)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestHostDNSResolverUsesDefaultResolverWhenNil(t *testing.T) {
	addresses, err := (HostDNSResolver{}).LookupHost(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("LookupHost(localhost): %v", err)
	}
	if len(addresses) == 0 {
		t.Fatal("expected at least one address for localhost")
	}
}

func TestHostDNSResolverUsesConfiguredResolver(t *testing.T) {
	resolver := &net.Resolver{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dial blocked")
		},
	}

	_, err := HostDNSResolver{Resolver: resolver}.LookupHost(context.Background(), "example.test")
	if err == nil || !strings.Contains(err.Error(), "dial blocked") {
		t.Fatalf("error = %v", err)
	}
}

func TestSystemdUnitInspectorFallsBackToDefaultRunner(t *testing.T) {
	var ran bool
	inspector := SystemdUnitInspector{
		defaultRun: func(_ context.Context, name string, args ...string) ([]byte, error) {
			ran = true
			if name != "systemctl" {
				t.Fatalf("command = %q, want systemctl", name)
			}
			return []byte("loaded\n"), nil
		},
	}

	exists, err := inspector.Exists(context.Background(), "veil-mieru.service")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("loaded unit reported missing")
	}
	if !ran {
		t.Fatal("default runner was not invoked")
	}
}

func TestSystemdUnitInspectorFallsBackToExecCommandRunner(t *testing.T) {
	original := ExecCommandRunner
	defer func() { ExecCommandRunner = original }()

	ExecCommandRunner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "systemctl" {
			t.Fatalf("command = %q, want systemctl", name)
		}
		return []byte("loaded\n"), nil
	}

	exists, err := (SystemdUnitInspector{}).Exists(context.Background(), "veil-mieru.service")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("loaded unit reported missing")
	}
}
