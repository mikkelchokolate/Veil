package firewall

import (
	"net"
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
		var port int
		var ok bool
		switch {
		case strings.EqualFold(fields[0], "Port"):
			port, ok = sshPortValue(fields[1])
		case strings.EqualFold(fields[0], "ListenAddress"):
			// sshd also binds ports via ListenAddress host:port and
			// [addr]:port forms — commonly with no Port directive at all.
			// Missing those ports would plan a UFW SSH allow for 22 while
			// sshd only listens on the ListenAddress port, locking the
			// operator out. Bare addresses without a port are skipped: they
			// bind the Port directives, which are already collected.
			port, ok = sshListenAddressPort(fields[1])
		default:
			continue
		}
		if !ok || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

func sshPortValue(value string) (int, bool) {
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// sshListenAddressPort extracts the explicit port from an sshd ListenAddress
// specification: IPv4 host:port, hostname:port, and bracketed IPv6
// [addr]:port forms. Bare addresses (including unbracketed IPv6) carry no
// port and report false.
func sshListenAddressPort(spec string) (int, bool) {
	if !strings.Contains(spec, ":") {
		return 0, false
	}
	_, portText, err := net.SplitHostPort(spec)
	if err != nil {
		return 0, false
	}
	return sshPortValue(portText)
}
