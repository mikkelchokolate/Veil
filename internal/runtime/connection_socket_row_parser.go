package runtime

import "strings"

type ConnectionSocketRow struct {
	Proto   string
	Address string
	Port    int
}

type ConnectionSocketRowParser struct{}

func NewConnectionSocketRowParser() ConnectionSocketRowParser { return ConnectionSocketRowParser{} }

func (ConnectionSocketRowParser) Parse(proto string, line string) (ConnectionSocketRow, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return ConnectionSocketRow{}, false
	}
	if (proto == "tcp" || proto == "tcp6") && fields[3] != "0A" {
		return ConnectionSocketRow{}, false
	}
	// UDP has no LISTEN state; a bound (unconnected) socket is identified by
	// an all-zero remote address. Connected UDP rows are clients or
	// established peers, not listeners.
	if (proto == "udp" || proto == "udp6") && !isAllZeroProcNetAddress(fields[2]) {
		return ConnectionSocketRow{}, false
	}
	addr, port := parseHexAddress(fields[1])
	if addr == "" || port == 0 {
		return ConnectionSocketRow{}, false
	}
	return ConnectionSocketRow{Proto: proto, Address: addr, Port: port}, true
}

// isAllZeroProcNetAddress reports whether a /proc/net/{tcp,udp}* address field
// (ADDR:PORT hex) is entirely zero — the bound-socket convention for an
// unconnected endpoint.
func isAllZeroProcNetAddress(field string) bool {
	if field == "" {
		return false
	}
	for _, r := range field {
		if r != '0' && r != ':' {
			return false
		}
	}
	return true
}
