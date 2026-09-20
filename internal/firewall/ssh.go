package firewall

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	sshConfigPaths = []string{"/etc/ssh/sshd_config"}
	sshConfigGlob  = "/etc/ssh/sshd_config.d/*.conf"
	readSSHFile    = os.ReadFile
	globSSHFiles   = filepath.Glob
)

// DetectSSHPorts returns the host SSH listen ports that must stay reachable
// before UFW is enabled. It defaults to 22 when sshd_config is missing or
// does not declare a Port.
func DetectSSHPorts() []int {
	paths := append([]string{}, sshConfigPaths...)
	if sshConfigGlob != "" {
		if matches, err := globSSHFiles(sshConfigGlob); err == nil {
			paths = append(paths, matches...)
		}
	}
	seen := map[int]bool{}
	var ports []int
	for _, path := range paths {
		data, err := readSSHFile(path)
		if err != nil {
			continue
		}
		for _, port := range parseSSHConfigPorts(data) {
			if seen[port] {
				continue
			}
			seen[port] = true
			ports = append(ports, port)
		}
	}
	if len(ports) == 0 {
		return []int{22}
	}
	return ports
}

func parseSSHConfigPorts(data []byte) []int {
	var ports []int
	seen := map[int]bool{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// sshd binds ports two ways: a bare "Port" directive and a
		// "ListenAddress" form carrying an explicit port (host:port or
		// [v6addr]:port). A config that only sets ListenAddress …:port and
		// no Port directive must still yield its port, otherwise the caller
		// defaults to 22 and UFW can lock the operator out (audit #355).
		var portText string
		switch {
		case strings.EqualFold(fields[0], "Port"):
			portText = fields[1]
		case strings.EqualFold(fields[0], "ListenAddress"):
			portText = sshListenAddressPort(fields[1])
		default:
			continue
		}
		if portText == "" {
			continue
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		if seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

// sshListenAddressPort extracts the explicit port from an OpenSSH
// ListenAddress value: "host:port", "ipv4:port", or "[v6addr]:port". A bare
// address carries no port — sshd then falls back to the Port directives — and
// an unbracketed IPv6 literal is an address, not address:port.
func sshListenAddressPort(addr string) string {
	if strings.HasPrefix(addr, "[") {
		end := strings.LastIndex(addr, "]")
		if end < 0 || end+2 > len(addr) || addr[end+1] != ':' {
			return ""
		}
		return addr[end+2:]
	}
	// Exactly one colon separates address from port; zero colons is a bare
	// host/IPv4 and more than one is an unbracketed IPv6 literal.
	if strings.Count(addr, ":") != 1 {
		return ""
	}
	return addr[strings.LastIndex(addr, ":")+1:]
}
