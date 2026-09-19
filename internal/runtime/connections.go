package runtime

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

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return findInodeByPortInSocketLines(proto, hexPort, lines)
}

// findInodeByPortInSocketLines locates the inode of the socket LISTENING on
// hexPort. The port must match the row's local address field only — a
// substring match could otherwise pick a connected socket whose *remote* port
// happens to equal the listen port and steal process attribution. The same
// listener filter as ConnectionSocketRowParser applies (TCP state 0A, UDP
// all-zero remote).
func findInodeByPortInSocketLines(proto, hexPort string, lines []string) string {
	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		localParts := strings.SplitN(fields[1], ":", 2)
		if len(localParts) != 2 || !strings.EqualFold(localParts[1], hexPort) {
			continue
		}
		if proto == "tcp" || proto == "tcp6" {
			if fields[3] != "0A" {
				continue
			}
		} else if proto == "udp" || proto == "udp6" {
			if !isAllZeroProcNetAddress(fields[2]) {
				continue
			}
		}
		return fields[9]
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
