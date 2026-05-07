package api

import (
	"fmt"
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
	ipHex, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return "", 0, false
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, false
	}
	addr = fmt.Sprintf("%d.%d.%d.%d", byte(ipHex), byte(ipHex>>8), byte(ipHex>>16), byte(ipHex>>24))
	return addr, int(port64), true
}

func parseHexAddress(hex string) (addr string, port int) {
	addr, port, ok := NewConnectionHexAddressParser().Parse(hex)
	if !ok {
		return "", 0
	}
	return addr, port
}
