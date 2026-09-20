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
	if (proto == "udp" || proto == "udp6") && !procNetRemoteIsWildcard(fields[2]) {
		// UDP rows carry no LISTEN state: a bound socket shows a wildcard
		// remote while a connected one carries its peer address. Reporting
		// connected rows would list outbound sessions as listeners (#336,
		// twin of the livevalidation occupancy fix #584).
		return ConnectionSocketRow{}, false
	}
	addr, port := parseHexAddress(fields[1])
	if addr == "" || port == 0 {
		return ConnectionSocketRow{}, false
	}
	return ConnectionSocketRow{Proto: proto, Address: addr, Port: port}, true
}

// procNetRemoteIsWildcard reports whether a /proc/net rem_address column is
// the all-zeros wildcard (00000000:0000 or its 32-hex IPv6 form), which marks
// an unconnected — i.e. bound/listening — socket.
func procNetRemoteIsWildcard(remoteAddress string) bool {
	return strings.Trim(remoteAddress, "0:") == ""
}
