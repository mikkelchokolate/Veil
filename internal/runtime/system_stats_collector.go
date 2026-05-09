package runtime

import "time"

type SystemStatsCollector struct {
	source systemStatsSource
}

type systemStatsSource interface {
	Meminfo() (memInfo, error)
	DiskStats(path string) (diskInfo, error)
	Uptime() (int64, error)
	LoadAvg() (loadAvg, error)
	CPUTicks() (idle, total uint64)
	Sleep(time.Duration)
}

func NewSystemStatsCollector(source systemStatsSource) SystemStatsCollector {
	return SystemStatsCollector{source: source}
}

func (c SystemStatsCollector) Read() (SystemStats, error) {
	stats := SystemStats{}
	if mem, err := c.source.Meminfo(); err == nil {
		stats.MemoryTotalMB = mem.total / 1024
		stats.MemoryUsedMB = mem.used / 1024
	}
	if disk, err := c.source.DiskStats("/"); err == nil {
		stats.DiskTotalGB = float64(disk.total) / 1024 / 1024 / 1024
		stats.DiskUsedGB = float64(disk.used) / 1024 / 1024 / 1024
	}
	if uptime, err := c.source.Uptime(); err == nil {
		stats.UptimeSeconds = uptime
	}
	if load, err := c.source.LoadAvg(); err == nil {
		stats.LoadAvg1 = load.avg1
		stats.LoadAvg5 = load.avg5
		stats.LoadAvg15 = load.avg15
	}
	stats.CPUPercent = c.cpuPercent()
	return stats, nil
}

func (c SystemStatsCollector) cpuPercent() float64 {
	prevIdle, prevTotal := c.source.CPUTicks()
	c.source.Sleep(100 * time.Millisecond)
	idle, total := c.source.CPUTicks()
	if total == prevTotal {
		return 0
	}
	deltaTotal := total - prevTotal
	deltaIdle := idle - prevIdle
	return (1.0 - float64(deltaIdle)/float64(deltaTotal)) * 100
}
