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
	if proto == "tcp" && fields[3] != "0A" {
		return ConnectionSocketRow{}, false
	}
	addr, port := parseHexAddress(fields[1])
	if addr == "" || port == 0 {
		return ConnectionSocketRow{}, false
	}
	return ConnectionSocketRow{Proto: proto, Address: addr, Port: port}, true
}
