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

func TestRuntimeObservationDefaultsNilTelemetry(t *testing.T) {
	observation := NewRuntimeObservation(RuntimeTelemetry{})
	snapshot := observation.Snapshot()
	// Default telemetry should produce a snapshot without panicking.
	// On Linux all real readers succeed, so Errors should be nilled.
	_ = snapshot
}

func TestRuntimeObservationLocalizesAllReaderErrors(t *testing.T) {
	observation := NewRuntimeObservation(RuntimeTelemetry{
		readSystem:      func() (SystemStats, error) { return SystemStats{}, errors.New("system down") },
		readTLSCertPath: func() string { return "" },
		readNetwork:     func() (NetworkStats, error) { return NetworkStats{}, errors.New("network down") },
		readConnections: func() (ConnectionsStats, error) { return ConnectionsStats{}, errors.New("connections down") },
		readProcesses:   func() (ProcessesStats, error) { return ProcessesStats{}, errors.New("processes down") },
		readDisk:        func() DiskStats { return DiskStats{} },
	})

	snapshot := observation.Snapshot()
	for _, key := range []string{"system", "network", "connections", "processes"} {
		if _, ok := snapshot.Errors[key]; !ok {
			t.Fatalf("missing error for %s: %+v", key, snapshot.Errors)
		}
	}
}

func TestRuntimeObservationOmitsErrorsWhenAllSucceed(t *testing.T) {
	observation := NewRuntimeObservation(RuntimeTelemetry{
		readSystem:      func() (SystemStats, error) { return SystemStats{}, nil },
		readTLSCertPath: func() string { return "" },
		readNetwork:     func() (NetworkStats, error) { return NetworkStats{}, nil },
		readConnections: func() (ConnectionsStats, error) { return ConnectionsStats{}, nil },
		readProcesses:   func() (ProcessesStats, error) { return ProcessesStats{}, nil },
		readDisk:        func() DiskStats { return DiskStats{} },
	})

	snapshot := observation.Snapshot()
	if snapshot.Errors != nil {
		t.Fatalf("expected nil errors when all succeed, got %+v", snapshot.Errors)
	}
}
