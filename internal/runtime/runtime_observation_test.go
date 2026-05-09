package runtime

import (
	"errors"
	"testing"
)

func TestRuntimeObservationCollectsSnapshotAndLocalizesReaderErrors(t *testing.T) {
	observation := NewRuntimeObservation(RuntimeTelemetry{
		readSystem:      func() (SystemStats, error) { return SystemStats{CPUPercent: 12.5}, nil },
		readTLSCertPath: func() string { return "" },
		readNetwork:     func() (NetworkStats, error) { return NetworkStats{}, errors.New("network unavailable") },
		readConnections: func() (ConnectionsStats, error) {
			return ConnectionsStats{Listeners: []ConnectionListener{{Proto: "tcp", Port: 2096}}}, nil
		},
		readProcesses: func() (ProcessesStats, error) { return ProcessesStats{Processes: []ProcessInfo{{Name: "veil"}}}, nil },
		readDisk:      func() DiskStats { return DiskStats{Dirs: []DirSizeInfo{{Path: "/etc/veil"}}} },
	})

	snapshot := observation.Snapshot()
	if snapshot.System.CPUPercent != 12.5 || len(snapshot.Connections.Listeners) != 1 || len(snapshot.Processes.Processes) != 1 || len(snapshot.Disk.Dirs) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Errors["network"] != "network unavailable" {
		t.Fatalf("snapshot errors = %+v", snapshot.Errors)
	}
}
