package api

type ConnectionDiscovery struct{}

func NewConnectionDiscovery() ConnectionDiscovery {
	return ConnectionDiscovery{}
}

func (ConnectionDiscovery) Read() (ConnectionsStats, error) {
	return readConnectionsStats()
}

func (ConnectionDiscovery) ReadListeningSockets(path, proto string) ([]ConnectionListener, error) {
	return readListeningSockets(path, proto)
}

func (ConnectionDiscovery) ParseHexAddress(value string) (string, int) {
	return parseHexAddress(value)
}

func (ConnectionDiscovery) FindProcessByPort(proto string, port int) string {
	return findProcessByPort(proto, port)
}
