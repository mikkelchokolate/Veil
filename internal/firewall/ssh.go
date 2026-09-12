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
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Port") {
			continue
		}
		port, err := strconv.Atoi(fields[1])
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
