package api

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
	procfs := NewRuntimeProcFS()
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
	return readTLSCert(t.readTLSCertPath())
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
