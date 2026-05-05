package api

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
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
	stats := SystemStats{}

	// Memory from /proc/meminfo
	if mem, err := readMeminfo(); err == nil {
		stats.MemoryTotalMB = mem.total / 1024
		stats.MemoryUsedMB = mem.used / 1024
	}

	// Disk from / (root) statfs
	if disk, err := readDiskStats("/"); err == nil {
		stats.DiskTotalGB = float64(disk.total) / 1024 / 1024 / 1024
		stats.DiskUsedGB = float64(disk.used) / 1024 / 1024 / 1024
	}

	// Uptime from /proc/uptime
	if uptime, err := readUptime(); err == nil {
		stats.UptimeSeconds = uptime
	}

	// Load average from /proc/loadavg
	if load, err := readLoadAvg(); err == nil {
		stats.LoadAvg1 = load.avg1
		stats.LoadAvg5 = load.avg5
		stats.LoadAvg15 = load.avg15
	}

	// CPU percentage (since last call)
	stats.CPUPercent = cpuPercent()

	return stats, nil
}

type memInfo struct{ total, used int64 }

func readMeminfo() (memInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return memInfo{}, err
	}
	defer f.Close()

	var total, avail, buffers, cached int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = parseKB(line)
		case strings.HasPrefix(line, "Buffers:"):
			buffers = parseKB(line)
		case strings.HasPrefix(line, "Cached:"):
			cached = parseKB(line)
		}
	}
	if total == 0 {
		return memInfo{}, nil
	}
	used := total - avail
	_ = buffers
	_ = cached
	return memInfo{total: total, used: used}, nil
}

type diskInfo struct{ total, used uint64 }

func readDiskStats(path string) (diskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskInfo{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free
	return diskInfo{total: total, used: used}, nil
}

func readUptime() (int64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, nil
	}
	secs, _ := strconv.ParseFloat(fields[0], 64)
	return int64(secs), nil
}

type loadAvg struct{ avg1, avg5, avg15 float64 }

func readLoadAvg() (loadAvg, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return loadAvg{}, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return loadAvg{}, nil
	}
	avg1, _ := strconv.ParseFloat(fields[0], 64)
	avg5, _ := strconv.ParseFloat(fields[1], 64)
	avg15, _ := strconv.ParseFloat(fields[2], 64)
	return loadAvg{avg1: avg1, avg5: avg5, avg15: avg15}, nil
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
	prevIdle, prevTotal := readCPUTicks()
	time.Sleep(100 * time.Millisecond)
	idle, total := readCPUTicks()

	if total == prevTotal {
		return 0
	}
	deltaTotal := total - prevTotal
	deltaIdle := idle - prevIdle
	return (1.0 - float64(deltaIdle)/float64(deltaTotal)) * 100
}

func readCPUTicks() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0
		}
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			total += v
			if i == 4 { // idle is the 4th field (0-indexed: 4)
				idle = v
			}
		}
		return idle, total
	}
	return 0, 0
}
