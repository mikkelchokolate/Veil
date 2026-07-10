package runtime

import "os"

type RuntimeTelemetry struct {
	readSystem      func() (SystemStats, error)
	readTLSCertPath func() string
	readNetwork     func() (NetworkStats, error)
	readConnections func() (ConnectionsStats, error)
	readProcesses   func() (ProcessesStats, error)
	readDisk        func() DiskStats
}

func NewRuntimeTelemetry() RuntimeTelemetry {
	return NewRuntimeTelemetryWithPolicy(NewManagedProcessPolicy())
}

// NewRuntimeTelemetryWithPolicy creates a telemetry reader that uses the given
// managed-process policy for process discovery. Callers that can reach the
// protocol registry should use this constructor so the policy reflects the
// installed set of protocol plugins.
func NewRuntimeTelemetryWithPolicy(policy ManagedProcessPolicy) RuntimeTelemetry {
	procfs := NewRuntimeProcFSWithPolicy(policy)
	return RuntimeTelemetry{
		readSystem:      procfs.System,
		readTLSCertPath: func() string { return os.Getenv("VEIL_TLS_CERT") },
		readNetwork:     procfs.Network,
		readConnections: procfs.Connections,
		readProcesses:   procfs.Processes,
		readDisk:        readDirDiskStats,
	}
}

func (t RuntimeTelemetry) System() (SystemStats, error) {
	return t.readSystem()
}

func (t RuntimeTelemetry) TLS() TLSCertInfo {
	return ReadTLSCert(t.readTLSCertPath())
}

func (t RuntimeTelemetry) Network() (NetworkStats, error) {
	return t.readNetwork()
}

func (t RuntimeTelemetry) Connections() (ConnectionsStats, error) {
	return t.readConnections()
}

func (t RuntimeTelemetry) Processes() (ProcessesStats, error) {
	return t.readProcesses()
}

func (t RuntimeTelemetry) Disk() DiskStats {
	return t.readDisk()
}

func (t RuntimeTelemetry) Observation() RuntimeObservationSnapshot {
	return NewRuntimeObservation(t).Snapshot()
}
