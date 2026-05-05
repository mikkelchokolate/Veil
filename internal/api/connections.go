package api

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ConnectionListener represents a listening socket.
type ConnectionListener struct {
	Proto   string `json:"proto"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Process string `json:"process,omitempty"`
}

// ConnectionsStats holds listening connection information.
type ConnectionsStats struct {
	Listeners []ConnectionListener `json:"listeners"`
}

// readConnectionsStats reads listening TCP/UDP sockets from /proc/net/tcp, /proc/net/udp.
func readConnectionsStats() (ConnectionsStats, error) {
	var stats ConnectionsStats
	tcp, _ := readListeningSockets("/proc/net/tcp", "tcp")
	stats.Listeners = append(stats.Listeners, tcp...)
	udp, _ := readListeningSockets("/proc/net/udp", "udp")
	stats.Listeners = append(stats.Listeners, udp...)
	return stats, nil
}

func readListeningSockets(path, proto string) ([]ConnectionListener, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var listeners []ConnectionListener
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		// State is field 3 for TCP, but for UDP field 3 is different
		localField := fields[1] // local_address:port in hex
		stateField := ""
		if proto == "tcp" && len(fields) > 3 {
			stateField = fields[3]
		}

		// Only listening sockets (0A = LISTEN for TCP; UDP always "listening")
		if proto == "tcp" && stateField != "0A" {
			continue
		}

		addr, port := parseHexAddress(localField)
		if addr == "" || port == 0 {
			continue
		}

		// Try to find process name
		process := findProcessByPort(proto, port)

		listeners = append(listeners, ConnectionListener{
			Proto:   proto,
			Address: addr,
			Port:    port,
			Process: process,
		})
	}
	return listeners, nil
}

func parseHexAddress(hex string) (addr string, port int) {
	parts := strings.Split(hex, ":")
	if len(parts) != 2 {
		return "", 0
	}
	// Parse hex IP (little-endian)
	ipHex, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return "", 0
	}
	addr = fmt.Sprintf("%d.%d.%d.%d", byte(ipHex), byte(ipHex>>8), byte(ipHex>>16), byte(ipHex>>24))
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return addr, 0
	}
	return addr, int(port64)
}

func findProcessByPort(proto string, port int) string {
	inode := findInodeByPort(proto, fmt.Sprintf("%04X", port))
	if inode == "" {
		return ""
	}
	return findProcessByInode(inode)
}

func findInodeByPort(proto, hexPort string) string {
	path := "/proc/net/" + proto
	if proto == "tcp" {
		path = "/proc/net/tcp"
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		line := scanner.Text()
		if strings.Contains(line, ":"+hexPort+" ") {
			fields := strings.Fields(line)
			if len(fields) >= 10 {
				return fields[9]
			}
		}
	}
	return ""
}

func findProcessByInode(inode string) string {
	procs, _ := os.ReadDir("/proc")
	for _, proc := range procs {
		if !proc.IsDir() {
			continue
		}
		pid := proc.Name()
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		fdDir := "/proc/" + pid + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil {
				continue
			}
			if strings.Contains(link, "socket:["+inode+"]") {
				// Read process name
				cmdline, _ := os.ReadFile("/proc/" + pid + "/comm")
				return strings.TrimSpace(string(cmdline))
			}
		}
	}
	return ""
}
