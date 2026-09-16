package runtime

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type ConnectionHexAddressParser struct{}

func NewConnectionHexAddressParser() ConnectionHexAddressParser { return ConnectionHexAddressParser{} }

func (ConnectionHexAddressParser) Parse(value string) (addr string, port int, ok bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", 0, false
	}
	addr = parseProcNetLocalAddress(parts[0])
	if addr == "" {
		return "", 0, false
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, false
	}
	return addr, int(port64), true
}

// parseProcNetLocalAddress decodes the /proc/net/{tcp,tcp6,udp,udp6} local
// address field. IPv4 rows carry one little-endian u32; IPv6 rows carry four
// little-endian u32 words printed contiguously.
func parseProcNetLocalAddress(hexAddr string) string {
	switch len(hexAddr) {
	case 8:
		ipHex, err := strconv.ParseUint(hexAddr, 16, 32)
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%d.%d.%d.%d", byte(ipHex), byte(ipHex>>8), byte(ipHex>>16), byte(ipHex>>24))
	case 32:
		ip := make(net.IP, net.IPv6len)
		for i := 0; i < 4; i++ {
			word, err := strconv.ParseUint(hexAddr[i*8:(i+1)*8], 16, 32)
			if err != nil {
				return ""
			}
			binary.LittleEndian.PutUint32(ip[i*4:], uint32(word))
		}
		return ip.String()
	default:
		return ""
	}
}

func parseHexAddress(hex string) (addr string, port int) {
	addr, port, ok := NewConnectionHexAddressParser().Parse(hex)
	if !ok {
		return "", 0
	}
	return addr, port
}
