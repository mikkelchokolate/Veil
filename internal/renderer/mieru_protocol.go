package renderer

import "strings"

func normalizeMieruProtocol(protocol string) string {
	return strings.ToUpper(protocol)
}
