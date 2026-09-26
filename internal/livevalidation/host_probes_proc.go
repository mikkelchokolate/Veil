package livevalidation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || os.IsPermission(err) {
		return true
	}
	return false
}

// isAddressUnavailable reports whether a probe bind failed because the host
// lacks that address family at all (IPv6 disabled, no IPv4 stack) — a
// condition that means the probe is inapplicable, not that the port is busy.
// EADDRINUSE and other errors still report the port as occupied.
func isAddressUnavailable(err error) bool {
	return errors.Is(err, syscall.EADDRNOTAVAIL) ||
		errors.Is(err, syscall.EAFNOSUPPORT) ||
		errors.Is(err, syscall.EPROTONOSUPPORT)
}

func (p HostPortProbe) availableFromProcNet(transport string, port int) (bool, error) {
	inUse, err := p.procNetBound(transport, port)
	if err != nil {
		return false, err
	}
	return !inUse, nil
}

func (p HostPortProbe) procNetBound(transport string, port int) (bool, error) {
	read := p.readProcNet
	if read == nil {
		read = readHostProcNet
	}

	names, proto, err := procNetTables(transport)
	if err != nil {
		return false, err
	}

	var lastErr error
	foundTable := false
	for _, name := range names {
		data, readErr := read(name)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			lastErr = readErr
			continue
		}
		foundTable = true
		if procNetContainsPort(data, port, proto) {
			return true, nil
		}
	}
	if !foundTable {
		if lastErr != nil {
			return false, fmt.Errorf("inspect %s port %d: %w", strings.ToUpper(transport), port, lastErr)
		}
		return false, fmt.Errorf("inspect %s port %d: /proc/net tables are unavailable", strings.ToUpper(transport), port)
	}
	return false, nil
}

func procNetTables(transport string) ([]string, string, error) {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "tcp":
		return []string{"tcp", "tcp6"}, "tcp", nil
	case "udp":
		return []string{"udp", "udp6"}, "udp", nil
	default:
		return nil, "", fmt.Errorf("unsupported transport %q", transport)
	}
}

func readHostProcNet(name string) ([]byte, error) {
	base := filepath.Base(name)
	if base != name || strings.Contains(base, string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid /proc/net table %q", name)
	}
	switch base {
	case "tcp", "tcp6", "udp", "udp6":
		return os.ReadFile("/proc/net/" + base)
	default:
		return nil, fmt.Errorf("invalid /proc/net table %q", name)
	}
}

func procNetContainsPort(data []byte, port int, proto string) bool {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		switch proto {
		case "tcp":
			// Only LISTEN rows (st 0A) hold a TCP port.
			if !strings.EqualFold(fields[3], "0A") {
				continue
			}
		case "udp":
			// UDP has no LISTEN state: a bound socket shows a wildcard remote
			// while a connected one carries the peer address. Counting
			// connected rows as busy would reject a free inbound port
			// whenever an outbound session happens to share its number (#584,
			// twin of the listening-ports fix #336).
			if !procNetRemoteIsWildcard(fields[2]) {
				continue
			}
		}
		localPort, ok := procNetLocalPort(fields[1])
		if ok && localPort == port {
			return true
		}
	}
	return false
}

// procNetRemoteIsWildcard reports whether a /proc/net rem_address column is
// the all-zeros wildcard (00000000:0000 or its 32-hex IPv6 form), which marks
// an unconnected — i.e. bound/listening — socket.
func procNetRemoteIsWildcard(remoteAddress string) bool {
	return strings.Trim(remoteAddress, "0:") == ""
}

func procNetLocalPort(localAddress string) (int, bool) {
	idx := strings.LastIndex(localAddress, ":")
	if idx < 0 || idx+1 >= len(localAddress) {
		return 0, false
	}
	port, err := strconv.ParseUint(localAddress[idx+1:], 16, 16)
	if err != nil || port == 0 {
		return 0, false
	}
	return int(port), true
}
