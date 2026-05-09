package runtime

type RuntimeProcFS struct{}

func NewRuntimeProcFS() RuntimeProcFS {
	return RuntimeProcFS{}
}

func (RuntimeProcFS) System() (SystemStats, error) {
	return readSystemStats()
}

func (RuntimeProcFS) Network() (NetworkStats, error) {
	return readNetworkStats()
}

func (RuntimeProcFS) Connections() (ConnectionsStats, error) {
	return NewConnectionDiscovery().Read()
}

func (RuntimeProcFS) Processes() (ProcessesStats, error) {
	return readProcessesStats()
}
