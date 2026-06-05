package livevalidation

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

type HostPortProbe struct{}

func (HostPortProbe) Available(ctx context.Context, transport string, port int) (bool, error) {
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
		listener, err := listenConfig.Listen(ctx, "tcp", address)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		}
		if err := listener.Close(); err != nil {
			return false, fmt.Errorf("close TCP probe: %w", err)
		}
		return true, nil
	case "udp":
		packet, err := listenConfig.ListenPacket(ctx, "udp", address)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		}
		if err := packet.Close(); err != nil {
			return false, fmt.Errorf("close UDP probe: %w", err)
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported transport %q", transport)
	}
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
}

func (i SystemdUnitInspector) Exists(ctx context.Context, unit string) (bool, error) {
	run := i.Run
	if run == nil {
		run = ExecCommandRunner
	}
	output, err := run(ctx, "systemctl", "show", "--property=LoadState", "--value", unit)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "loaded", nil
}

func ExecCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
