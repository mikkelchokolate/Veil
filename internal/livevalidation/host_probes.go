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

// probeBindHosts are the wildcard addresses Veil's runtimes bind. A wildcard
// bind conflicts with ANY existing listener on the port — including one bound
// to a specific non-loopback address (192.168.x.x:port) or the IPv6 loopback
// (::1:port) — while a loopback-only probe would happily succeed past them
// and let the runtime's own wildcard bind fail at apply (#1031). The IPv6
// probe is skipped when the host has no IPv6 stack.
var probeBindHosts = []string{"0.0.0.0", "::"}

// availableAfterBindsSucceeded cross-checks /proc/net after the wildcard
// binds all succeeded. A successful bind alone is not conclusive on Linux:
// SO_REUSEADDR (set on every Go socket) lets a wildcard bind coexist with a
// socket bound to a specific address on the same port once BOTH sockets opt
// into reuse — UDP most notably, where no LISTEN state forces exclusivity —
// so the incumbent shadows the runtime on that address. /proc/net lists the
// bound socket regardless of reuse flags (#1031). When the tables are
// unavailable (non-Linux host) the successful wildcard binds are the only
// evidence available and stand.
func (p HostPortProbe) availableAfterBindsSucceeded(transport string, port int) (bool, error) {
	inUse, err := p.procNetBound(transport, port)
	if err != nil {
		return true, nil
	}
	return !inUse, nil
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

	var listenConfig net.ListenConfig
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "tcp":
		listen := p.listenTCP
		if listen == nil {
			listen = func(ctx context.Context, lc *net.ListenConfig, addr string) (net.Listener, error) {
				return lc.Listen(ctx, "tcp", addr)
			}
		}
		for _, host := range probeBindHosts {
			listener, err := listen(ctx, &listenConfig, net.JoinHostPort(host, strconv.Itoa(port)))
			if err != nil {
				if isAddressUnavailable(err) {
					continue
				}
				return p.availableAfterListenError(ctx, "tcp", port, err)
			}
			if err := listener.Close(); err != nil {
				return false, fmt.Errorf("close TCP probe: %w", err)
			}
		}
		return p.availableAfterBindsSucceeded("tcp", port)
	case "udp":
		listen := p.listenUDP
		if listen == nil {
			listen = func(ctx context.Context, lc *net.ListenConfig, addr string) (net.PacketConn, error) {
				return lc.ListenPacket(ctx, "udp", addr)
			}
		}
		for _, host := range probeBindHosts {
			packet, err := listen(ctx, &listenConfig, net.JoinHostPort(host, strconv.Itoa(port)))
			if err != nil {
				if isAddressUnavailable(err) {
					continue
				}
				return p.availableAfterListenError(ctx, "udp", port, err)
			}
			if err := packet.Close(); err != nil {
				return false, fmt.Errorf("close UDP probe: %w", err)
			}
		}
		return p.availableAfterBindsSucceeded("udp", port)
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
