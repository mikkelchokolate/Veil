package runtime

type ProcessDiscovery struct {
	source processSource
	policy ManagedProcessPolicy
}

type processSource interface {
	PIDs() ([]int, error)
	SystemUptime() (int64, error)
	Name(pid int) string
	MemoryMB(pid int) int64
	CPUPercent(pid int, uptimeSec int64) float64
	UptimeSeconds(pid int, systemUptime int64) int64
}

// NewProcessDiscovery creates a discovery reader that filters processes through
// the given policy.
func NewProcessDiscovery(source processSource, policy ManagedProcessPolicy) ProcessDiscovery {
	return ProcessDiscovery{source: source, policy: policy}
}

func (d ProcessDiscovery) Read() (ProcessesStats, error) {
	var stats ProcessesStats
	pids, err := d.source.PIDs()
	if err != nil {
		return stats, err
	}
	uptimeSec, _ := d.source.SystemUptime()
	for _, pid := range pids {
		name := d.source.Name(pid)
		if !d.policy.IsManaged(name) {
			continue
		}
		stats.Processes = append(stats.Processes, ProcessInfo{
			PID:           pid,
			Name:          name,
			CPUPercent:    d.source.CPUPercent(pid, uptimeSec),
			MemoryMB:      d.source.MemoryMB(pid),
			UptimeSeconds: d.source.UptimeSeconds(pid, uptimeSec),
		})
	}
	return stats, nil
}
