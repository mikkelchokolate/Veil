package livevalidation

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestHostPortProbeDetectsBusyTCPPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen TCP: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	available, err := (HostPortProbe{}).Available(context.Background(), "tcp", port)
	if err != nil {
		t.Fatalf("probe TCP: %v", err)
	}
	if available {
		t.Fatalf("busy TCP port %d reported available", port)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("close TCP listener: %v", err)
	}
	available, err = (HostPortProbe{}).Available(context.Background(), "tcp", port)
	if err != nil {
		t.Fatalf("probe released TCP: %v", err)
	}
	if !available {
		t.Fatalf("released TCP port %d reported busy", port)
	}
}

func TestHostPortProbeDetectsBusyUDPPort(t *testing.T) {
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	port := packet.LocalAddr().(*net.UDPAddr).Port

	available, err := (HostPortProbe{}).Available(context.Background(), "udp", port)
	if err != nil {
		t.Fatalf("probe UDP: %v", err)
	}
	if available {
		t.Fatalf("busy UDP port %d reported available", port)
	}

	if err := packet.Close(); err != nil {
		t.Fatalf("close UDP listener: %v", err)
	}
	available, err = (HostPortProbe{}).Available(context.Background(), "udp", port)
	if err != nil {
		t.Fatalf("probe released UDP: %v", err)
	}
	if !available {
		t.Fatalf("released UDP port %d reported busy", port)
	}
}

func TestHostPortProbeRejectsUnknownTransport(t *testing.T) {
	_, err := (HostPortProbe{}).Available(context.Background(), "sctp", 443)
	if err == nil || !strings.Contains(err.Error(), "unsupported transport") {
		t.Fatalf("error = %v", err)
	}
}

func TestHostPortProbeRejectsInvalidPort(t *testing.T) {
	for _, port := range []int{0, 65536} {
		_, err := (HostPortProbe{}).Available(context.Background(), "tcp", port)
		if err == nil || !strings.Contains(err.Error(), strconv.Itoa(port)) {
			t.Fatalf("port %d error = %v", port, err)
		}
	}
}

func TestHostBinaryLookupUsesExecutablePath(t *testing.T) {
	path, err := (HostBinaryLookup{}).LookPath("go")
	if err != nil {
		t.Fatalf("LookPath(go): %v", err)
	}
	if path == "" {
		t.Fatal("LookPath(go) returned an empty path")
	}
}

func TestSystemdUnitInspectorChecksLoadState(t *testing.T) {
	var command string
	var arguments []string
	inspector := SystemdUnitInspector{
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			command = name
			arguments = append([]string(nil), args...)
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
	if command != "systemctl" {
		t.Fatalf("command = %q", command)
	}
	wantArgs := []string{"show", "--property=LoadState", "--value", "veil-mieru.service"}
	if strings.Join(arguments, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("arguments = %v, want %v", arguments, wantArgs)
	}
}

func TestSystemdUnitInspectorTreatsNotFoundAsMissing(t *testing.T) {
	inspector := SystemdUnitInspector{
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("not-found\n"), nil
		},
	}

	exists, err := inspector.Exists(context.Background(), "missing.service")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("not-found unit reported present")
	}
}

func TestSystemdUnitInspectorPropagatesRunnerFailure(t *testing.T) {
	want := errors.New("systemctl unavailable")
	inspector := SystemdUnitInspector{
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, want
		},
	}

	_, err := inspector.Exists(context.Background(), "veil.service")
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestExecCommandRunnerHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ExecCommandRunner(ctx, exec.Command("go").Path, "version")
	if err == nil {
		t.Fatal("cancelled command unexpectedly succeeded")
	}
}
