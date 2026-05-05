package api

import (
	"os"
	"strconv"
	"strings"
)

// ProcessInfo holds information about a running process.
type ProcessInfo struct {
	PID            int     `json:"pid"`
	Name           string  `json:"name"`
	CPUPercent     float64 `json:"cpuPercent"`
	MemoryMB       int64   `json:"memoryMB"`
	UptimeSeconds  int64   `json:"uptimeSeconds"`
}

// ProcessesStats holds process information for managed services.
type ProcessesStats struct {
	Processes []ProcessInfo `json:"processes"`
}

var managedProcessNames = []string{"caddy", "hysteria2", "sing-box", "veil"}

// readProcessesStats finds managed service processes via /proc.
func readProcessesStats() (ProcessesStats, error) {
	var stats ProcessesStats
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return stats, err
	}

	uptimeSec, _ := readSystemUptime()
	for _, proc := range procs {
		if !proc.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(proc.Name())
		if err != nil {
			continue
		}
		name := readProcessName(pid)
		if !isManagedProcess(name) {
			continue
		}
		cpu := readProcessCPU(pid, uptimeSec)
		mem := readProcessMemory(pid)
		uptime := readProcessUptime(pid, uptimeSec)

		stats.Processes = append(stats.Processes, ProcessInfo{
			PID:           pid,
			Name:          name,
			CPUPercent:    cpu,
			MemoryMB:      mem,
			UptimeSeconds: uptime,
		})
	}
	return stats, nil
}

func isManagedProcess(name string) bool {
	for _, n := range managedProcessNames {
		if name == n {
			return true
		}
	}
	return false
}

func readProcessName(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readProcessMemory(pid int) int64 {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	// RSS in pages (field 1), convert to MB (page size = 4096)
	rssPages, _ := strconv.ParseInt(fields[1], 10, 64)
	return rssPages * 4 / 1024 // pages * 4KB / 1024 = MB
}

func readProcessCPU(pid int, uptimeSec int64) float64 {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	// Format: pid (comm) state ... utime stime cutime cstime starttime ...
	// Find closing paren, then parse fields after
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 {
		return 0
	}
	fields := strings.Fields(string(data[closeParen+2:]))
	if len(fields) < 14 {
		return 0
	}
	utime, _ := strconv.ParseInt(fields[11], 10, 64)  // 14th field after ')'
	stime, _ := strconv.ParseInt(fields[12], 10, 64)
	starttime, _ := strconv.ParseInt(fields[19], 10, 64) // 22nd field

	totalTicks := utime + stime
	clkTck := int64(100) // sysconf(_SC_CLK_TCK) = 100
	if uptimeSec <= 0 {
		return 0
	}
	seconds := uptimeSec - starttime/clkTck
	if seconds <= 0 {
		return 0
	}
	return float64(totalTicks) / float64(clkTck) / float64(seconds) * 100
}

func readProcessUptime(pid int, systemUptime int64) int64 {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 {
		return 0
	}
	fields := strings.Fields(string(data[closeParen+2:]))
	if len(fields) < 20 {
		return 0
	}
	starttime, _ := strconv.ParseInt(fields[19], 10, 64)
	clkTck := int64(100)
	return systemUptime - starttime/clkTck
}

func readSystemUptime() (int64, error) {
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
