package api

type RuntimeObservation struct {
	telemetry RuntimeTelemetry
}

type RuntimeObservationSnapshot struct {
	System      SystemStats       `json:"system"`
	TLS         TLSCertInfo       `json:"tls"`
	Network     NetworkStats      `json:"network"`
	Connections ConnectionsStats  `json:"connections"`
	Processes   ProcessesStats    `json:"processes"`
	Disk        DiskStats         `json:"disk"`
	Errors      map[string]string `json:"errors,omitempty"`
}

func NewRuntimeObservation(telemetry RuntimeTelemetry) RuntimeObservation {
	if telemetry.readSystem == nil || telemetry.readTLSCertPath == nil || telemetry.readNetwork == nil || telemetry.readConnections == nil || telemetry.readProcesses == nil || telemetry.readDisk == nil {
		telemetry = NewRuntimeTelemetry()
	}
	return RuntimeObservation{telemetry: telemetry}
}

func (o RuntimeObservation) Snapshot() RuntimeObservationSnapshot {
	snapshot := RuntimeObservationSnapshot{Errors: map[string]string{}}
	if system, err := o.telemetry.System(); err != nil {
		snapshot.Errors["system"] = err.Error()
	} else {
		snapshot.System = system
	}
	snapshot.TLS = o.telemetry.TLS()
	if network, err := o.telemetry.Network(); err != nil {
		snapshot.Errors["network"] = err.Error()
	} else {
		snapshot.Network = network
	}
	if connections, err := o.telemetry.Connections(); err != nil {
		snapshot.Errors["connections"] = err.Error()
	} else {
		snapshot.Connections = connections
	}
	if processes, err := o.telemetry.Processes(); err != nil {
		snapshot.Errors["processes"] = err.Error()
	} else {
		snapshot.Processes = processes
	}
	snapshot.Disk = o.telemetry.Disk()
	if len(snapshot.Errors) == 0 {
		snapshot.Errors = nil
	}
	return snapshot
}
