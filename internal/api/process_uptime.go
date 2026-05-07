package api

type ProcessUptime struct {
	clockTicksPerSecond int64
}

func NewProcessUptime(clockTicksPerSecond int64) ProcessUptime {
	if clockTicksPerSecond <= 0 {
		clockTicksPerSecond = 100
	}
	return ProcessUptime{clockTicksPerSecond: clockTicksPerSecond}
}

func (u ProcessUptime) Seconds(stat ProcessStatFields, systemUptimeSeconds int64) int64 {
	return systemUptimeSeconds - stat.StartTimeTicks/u.clockTicksPerSecond
}
