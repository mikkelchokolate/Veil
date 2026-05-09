package api

import veilruntime "github.com/veil-panel/veil/internal/runtime"

type SystemStats = veilruntime.SystemStats
type TLSCertInfo = veilruntime.TLSCertInfo
type NetworkInterface = veilruntime.NetworkInterface
type NetworkStats = veilruntime.NetworkStats
type ConnectionListener = veilruntime.ConnectionListener
type ConnectionsStats = veilruntime.ConnectionsStats
type ProcessInfo = veilruntime.ProcessInfo
type ProcessesStats = veilruntime.ProcessesStats
type DirSizeInfo = veilruntime.DirSizeInfo
type DiskStats = veilruntime.DiskStats
type RuntimeTelemetry = veilruntime.RuntimeTelemetry
type RuntimeObservation = veilruntime.RuntimeObservation
type RuntimeObservationSnapshot = veilruntime.RuntimeObservationSnapshot
type RuntimeProcFS = veilruntime.RuntimeProcFS
type RuntimeCommandInput = veilruntime.RuntimeCommandInput
type RuntimeCommandOutput = veilruntime.RuntimeCommandOutput
type RuntimeCommandExecutor = veilruntime.RuntimeCommandExecutor

func NewRuntimeTelemetry() RuntimeTelemetry { return veilruntime.NewRuntimeTelemetry() }

func NewRuntimeObservation(telemetry RuntimeTelemetry) RuntimeObservation {
	return veilruntime.NewRuntimeObservation(telemetry)
}

func NewRuntimeProcFS() RuntimeProcFS { return veilruntime.NewRuntimeProcFS() }

func NewRuntimeCommandExecutor() RuntimeCommandExecutor {
	return veilruntime.NewRuntimeCommandExecutor()
}

func readTLSCert(path string) TLSCertInfo { return veilruntime.ReadTLSCert(path) }

func isManagedProcess(name string) bool { return veilruntime.NewManagedProcessPolicy().IsManaged(name) }
