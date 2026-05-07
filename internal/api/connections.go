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
	return NewConnectionDiscovery().Read()
}

func readListeningSockets(path, proto string) ([]ConnectionListener, error) {
	return newConnectionDiscoveryWithSource(fileConnectionSource{path: path}).listeningSockets(proto)
}

func findProcessByPort(proto string, port int) string {
	inode := findInodeByPort(proto, fmt.Sprintf("%04X", port))
	if inode == "" {
		return ""
	}
	return findProcessByInode(inode)
}

func findInodeByPort(proto, hexPort string) string {
	f, err := os.Open(NewConnectionProcSocketPath().ForProtocol(proto))
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
