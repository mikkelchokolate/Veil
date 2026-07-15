package runtime

type RuntimeProcFS struct {
	policy ManagedProcessPolicy
}

// NewRuntimeProcFSWithPolicy creates a procfs reader that filters managed
// processes through the given policy.
func NewRuntimeProcFSWithPolicy(policy ManagedProcessPolicy) RuntimeProcFS {
	return RuntimeProcFS{policy: policy}
}

func (r RuntimeProcFS) System() (SystemStats, error) {
	return readSystemStats()
}

func (r RuntimeProcFS) Network() (NetworkStats, error) {
	return readNetworkStats()
}

func (r RuntimeProcFS) Connections() (ConnectionsStats, error) {
	return NewConnectionDiscovery().Read()
}

func (r RuntimeProcFS) Processes() (ProcessesStats, error) {
	return readProcessesStats(r.policy)
}
