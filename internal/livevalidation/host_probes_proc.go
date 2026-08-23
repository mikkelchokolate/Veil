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

	names, listenOnly, err := procNetTables(transport)
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
		if procNetContainsPort(data, port, listenOnly) {
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

func procNetTables(transport string) ([]string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "tcp":
		return []string{"tcp", "tcp6"}, true, nil
	case "udp":
		return []string{"udp", "udp6"}, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported transport %q", transport)
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

func procNetContainsPort(data []byte, port int, listenOnly bool) bool {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if listenOnly && !strings.EqualFold(fields[3], "0A") {
			continue
		}
		localPort, ok := procNetLocalPort(fields[1])
		if ok && localPort == port {
			return true
		}
	}
	return false
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
