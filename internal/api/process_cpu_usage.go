package api

type ProcessCPUUsage struct {
	clockTicksPerSecond int64
}

func NewProcessCPUUsage(clockTicksPerSecond int64) ProcessCPUUsage {
	if clockTicksPerSecond <= 0 {
		clockTicksPerSecond = 100
	}
	return ProcessCPUUsage{clockTicksPerSecond: clockTicksPerSecond}
}

func (u ProcessCPUUsage) Percent(stat ProcessStatFields, systemUptimeSeconds int64) float64 {
	if systemUptimeSeconds <= 0 {
		return 0
	}
	seconds := systemUptimeSeconds - stat.StartTimeTicks/u.clockTicksPerSecond
	if seconds <= 0 {
		return 0
	}
	totalTicks := stat.UserTicks + stat.SystemTicks
	return float64(totalTicks) / float64(u.clockTicksPerSecond) / float64(seconds) * 100
}
