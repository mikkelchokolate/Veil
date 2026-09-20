package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
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
	// Listeners must serialize as [] (never null): clean minimal environments
	// (CI containers without any host listener) otherwise emit
	// "listeners": null and break API consumers.
	stats := ConnectionsStats{Listeners: []ConnectionListener{}}
	// Dual-stack sockets (e.g. Hysteria2 listen :<port>) appear only in the
	// tcp6/udp6 tables, so all four /proc tables must be scanned.
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		listeners, err := d.listeningSockets(proto)
		if err != nil {
			// A missing table means the kernel lacks that stack — there are
			// genuinely no listeners to report. Any other read failure
			// (EACCES, IO) must surface: pretending the table was empty
			// would silently under-report IPv6/dual-stack listeners (#587).
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return stats, fmt.Errorf("read %s socket table: %w", proto, err)
		}
		stats.Listeners = append(stats.Listeners, listeners...)
	}
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
