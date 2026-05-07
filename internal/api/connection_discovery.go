package api

import (
	"bufio"
	"os"
	"strings"
)

type ConnectionDiscovery struct {
	source connectionSource
}

type connectionSource interface {
	SocketLines(proto string) ([]string, error)
	ProcessByPort(proto string, port int) string
}

func NewConnectionDiscovery() ConnectionDiscovery {
	return newConnectionDiscoveryWithSource(procConnectionSource{})
}

func newConnectionDiscoveryWithSource(source connectionSource) ConnectionDiscovery {
	return ConnectionDiscovery{source: source}
}

func (d ConnectionDiscovery) Read() (ConnectionsStats, error) {
	var stats ConnectionsStats
	tcp, _ := d.listeningSockets("tcp")
	stats.Listeners = append(stats.Listeners, tcp...)
	udp, _ := d.listeningSockets("udp")
	stats.Listeners = append(stats.Listeners, udp...)
	return stats, nil
}

func (d ConnectionDiscovery) listeningSockets(proto string) ([]ConnectionListener, error) {
	lines, err := d.source.SocketLines(proto)
	if err != nil {
		return nil, err
	}
	listeners := make([]ConnectionListener, 0)
	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		stateField := ""
		if proto == "tcp" && len(fields) > 3 {
			stateField = fields[3]
		}
		if proto == "tcp" && stateField != "0A" {
			continue
		}
		addr, port := parseHexAddress(fields[1])
		if addr == "" || port == 0 {
			continue
		}
		listeners = append(listeners, ConnectionListener{
			Proto:   proto,
			Address: addr,
			Port:    port,
			Process: d.source.ProcessByPort(proto, port),
		})
	}
	return listeners, nil
}

func (d ConnectionDiscovery) ReadListeningSockets(path, proto string) ([]ConnectionListener, error) {
	return readListeningSockets(path, proto)
}

func (ConnectionDiscovery) ParseHexAddress(value string) (string, int) {
	return parseHexAddress(value)
}

func (ConnectionDiscovery) FindProcessByPort(proto string, port int) string {
	return findProcessByPort(proto, port)
}

type procConnectionSource struct{}

func (procConnectionSource) SocketLines(proto string) ([]string, error) {
	path := "/proc/net/" + proto
	if proto == "tcp" {
		path = "/proc/net/tcp"
	}
	return readLines(path)
}

func (procConnectionSource) ProcessByPort(proto string, port int) string {
	return findProcessByPort(proto, port)
}

type fileConnectionSource struct {
	path string
}

func (s fileConnectionSource) SocketLines(proto string) ([]string, error) { return readLines(s.path) }

func (fileConnectionSource) ProcessByPort(proto string, port int) string {
	return findProcessByPort(proto, port)
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
