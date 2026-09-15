package installer

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// randomReader is overridable in tests so that RandomHighPort error paths can be
// exercised without mocking the crypto/rand package.
var randomReader = rand.Read

const (
	RandomPortMin = 20000
	RandomPortMax = 50000
	// DefaultUnprivilegedPortStart is Linux's documented default for
	// net.ipv4.ip_unprivileged_port_start. veil.service runs as User=veil with
	// an empty capability set, so it cannot bind ports below this floor.
	DefaultUnprivilegedPortStart = 1024
)

var unprivilegedPortStart = readIPUnprivilegedPortStart

func readIPUnprivilegedPortStart() int {
	body, err := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start")
	if err != nil {
		return DefaultUnprivilegedPortStart
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || n < 0 || n > 65535 {
		return DefaultUnprivilegedPortStart
	}
	return n
}

func rejectPrivilegedPanelPort(port int) error {
	floor := unprivilegedPortStart()
	if port < floor {
		return fmt.Errorf("panel port %d is privileged (host unprivileged port start is %d); veil.service runs as User=veil without CAP_NET_BIND_SERVICE and cannot bind it", port, floor)
	}
	return nil
}

func RandomHighPort() (int, error) {
	var b [8]byte
	if _, err := randomReader(b[:]); err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint64(b[:])
	span := uint64(RandomPortMax - RandomPortMin + 1)
	return RandomPortMin + int(n%span), nil
}

func SelectPanelPort(requested int, randomPort func() (int, error)) (port int, random bool, err error) {
	if requested < 0 || requested > 65535 {
		return 0, false, fmt.Errorf("invalid panel port %d", requested)
	}
	if requested > 0 {
		if err := rejectPrivilegedPanelPort(requested); err != nil {
			return 0, false, err
		}
		return requested, false, nil
	}
	if randomPort == nil {
		randomPort = RandomHighPort
	}
	port, err = randomPort()
	if err != nil {
		return 0, false, err
	}
	if port <= 0 || port > 65535 {
		return 0, false, fmt.Errorf("random panel port is invalid: %d", port)
	}
	if err := rejectPrivilegedPanelPort(port); err != nil {
		return 0, false, err
	}
	return port, true, nil
}
