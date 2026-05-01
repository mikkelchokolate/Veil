package installer

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
)

const (
	RandomPortMin = 20000
	RandomPortMax = 50000
)

func DetectPortAvailability(ports []int) (PortAvailability, error) {
	availability := PortAvailability{
		TCPBusy: map[int]bool{},
		UDPBusy: map[int]bool{},
	}
	for _, port := range ports {
		if port <= 0 || port > 65535 {
			return availability, fmt.Errorf("invalid port %d", port)
		}
		availability.TCPBusy[port] = isTCPBusy(port)
		availability.UDPBusy[port] = isUDPBusy(port)
	}
	return availability, nil
}

func RandomHighPort() (int, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint64(b[:])
	span := uint64(RandomPortMax - RandomPortMin + 1)
	return RandomPortMin + int(n%span), nil
}

func isTCPBusy(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

func isUDPBusy(port int) bool {
	conn, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}
