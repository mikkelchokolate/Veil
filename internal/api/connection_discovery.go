package api

import (
	"bufio"
	"os"
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
	parser := NewConnectionSocketRowParser()
	for _, line := range lines {
		row, ok := parser.Parse(proto, line)
		if !ok {
			continue
		}
		listeners = append(listeners, ConnectionListener{
			Proto:   row.Proto,
			Address: row.Address,
			Port:    row.Port,
			Process: d.source.ProcessByPort(proto, row.Port),
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
	return readLines(NewConnectionProcSocketPath().ForProtocol(proto))
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
