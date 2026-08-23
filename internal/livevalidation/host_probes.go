package livevalidation

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

type HostPortProbe struct {
	// listenTCP and listenUDP are test hooks. When nil the probe uses the
	// standard net.ListenConfig implementation.
	listenTCP func(context.Context, *net.ListenConfig, string) (net.Listener, error)
	listenUDP func(context.Context, *net.ListenConfig, string) (net.PacketConn, error)
	// readProcNet is a test hook for /proc/net/<name>. When nil the probe
	// reads /proc/net from the host.
	readProcNet func(string) ([]byte, error)
}

func (p HostPortProbe) Available(ctx context.Context, transport string, port int) (bool, error) {
	if port < 1 || port > 65535 {
		return false, fmt.Errorf("invalid port %d", port)
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var listenConfig net.ListenConfig
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "tcp":
		listen := p.listenTCP
		if listen == nil {
			listen = func(ctx context.Context, lc *net.ListenConfig, addr string) (net.Listener, error) {
				return lc.Listen(ctx, "tcp", addr)
			}
		}
		listener, err := listen(ctx, &listenConfig, address)
		if err != nil {
			return p.availableAfterListenError(ctx, "tcp", port, err)
		}
		if err := listener.Close(); err != nil {
			return false, fmt.Errorf("close TCP probe: %w", err)
		}
		return true, nil
	case "udp":
		listen := p.listenUDP
		if listen == nil {
			listen = func(ctx context.Context, lc *net.ListenConfig, addr string) (net.PacketConn, error) {
				return lc.ListenPacket(ctx, "udp", addr)
			}
		}
		packet, err := listen(ctx, &listenConfig, address)
		if err != nil {
			return p.availableAfterListenError(ctx, "udp", port, err)
		}
		if err := packet.Close(); err != nil {
			return false, fmt.Errorf("close UDP probe: %w", err)
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported transport %q", transport)
	}
}

func (p HostPortProbe) availableAfterListenError(ctx context.Context, transport string, port int, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	// The panel process has no CAP_NET_BIND_SERVICE, so a bind of a
	// privileged port fails with EACCES even when the port is free.
	// Hysteria2/Naive/Mieru units do have that capability, so report
	// occupancy from /proc/net instead of treating EACCES as "in use".
	if isPermissionDenied(err) {
		return p.availableFromProcNet(transport, port)
	}
	return false, nil
}

type HostDNSResolver struct {
	Resolver *net.Resolver
}

func (r HostDNSResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return resolver.LookupHost(ctx, host)
}

type HostBinaryLookup struct{}

func (HostBinaryLookup) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

type CommandRunner func(context.Context, string, ...string) ([]byte, error)

type SystemdUnitInspector struct {
	Run CommandRunner
	// defaultRun is a test hook used when Run is nil. It defaults to
	// ExecCommandRunner so production code does not depend on the hook.
	defaultRun CommandRunner
}

func (i SystemdUnitInspector) Exists(ctx context.Context, unit string) (bool, error) {
	run := i.Run
	if run == nil {
		run = i.defaultRun
	}
	if run == nil {
		run = ExecCommandRunner
	}
	output, err := run(ctx, "systemctl", "show", "--property=LoadState", "--value", unit)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "loaded", nil
}

// ExecCommandRunner is the production command runner. It is a variable so
// tests can temporarily replace it to exercise the final fallback path in
// SystemdUnitInspector.Exists.
var ExecCommandRunner CommandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
