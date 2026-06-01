package runtime

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// SystemStats holds system resource metrics.
type SystemStats struct {
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsedMB  int64   `json:"memoryUsedMB"`
	MemoryTotalMB int64   `json:"memoryTotalMB"`
	DiskUsedGB    float64 `json:"diskUsedGB"`
	DiskTotalGB   float64 `json:"diskTotalGB"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
	LoadAvg1      float64 `json:"loadAvg1"`
	LoadAvg5      float64 `json:"loadAvg5"`
	LoadAvg15     float64 `json:"loadAvg15"`
}

// readSystemStats collects CPU, memory, disk, and load metrics.
func readSystemStats() (SystemStats, error) {
	return NewSystemStatsCollector(procSystemStatsSource{}).Read()
}

type procSystemStatsSource struct{}

func (procSystemStatsSource) Meminfo() (memInfo, error) { return readMeminfo() }

func (procSystemStatsSource) DiskStats(path string) (diskInfo, error) { return readDiskStats(path) }

func (procSystemStatsSource) Uptime() (int64, error) { return readUptime() }

func (procSystemStatsSource) LoadAvg() (loadAvg, error) { return readLoadAvg() }

func (procSystemStatsSource) Sleep(duration time.Duration) { time.Sleep(duration) }

type memInfo struct{ total, used int64 }

func readMeminfo() (memInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return memInfo{}, err
	}
	return NewMeminfoParser().Parse(string(data)), nil
}

type diskInfo struct{ total, used uint64 }

func readUptime() (int64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	return NewUptimeParser().Parse(string(data)), nil
}

type loadAvg struct{ avg1, avg5, avg15 float64 }

func readLoadAvg() (loadAvg, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return loadAvg{}, err
	}
	return NewLoadAvgParser().Parse(string(data)), nil
}

func parseKB(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseInt(fields[1], 10, 64)
	return v
}

// cpuPercent returns a rough CPU usage percentage computed from /proc/stat.
// It stores the previous tick values and returns the delta since last call.
func cpuPercent() float64 {
	return NewSystemStatsCollector(procSystemStatsSource{}).cpuPercent()
}

func readCPUTicks() (idle, total uint64) {
	return procSystemStatsSource{}.CPUTicks()
}

func (procSystemStatsSource) CPUTicks() (idle, total uint64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	return NewCPUTicksParser().Parse(string(data))
}
