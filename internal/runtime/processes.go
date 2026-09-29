package runtime

import (
	"os"
	"strconv"
	"strings"
)

// ProcessInfo holds information about a running process.
//
// CPUPercent is a live rate — Δ(utime+stime)/Δwall since the previous sample
// taken by the shared ProcessCPUSampler — not a since-start average (#1190).
// A pid with no previous sample yet reports the lifetime average so the value
// stays a finite non-negative number for JSON consumers.
type ProcessInfo struct {
	PID           int     `json:"pid"`
	Name          string  `json:"name"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryMB      int64   `json:"memoryMB"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
}

// ProcessesStats holds process information for managed services.
type ProcessesStats struct {
	Processes []ProcessInfo `json:"processes"`
}

// defaultProcessCPUSampler is shared across requests: /api/processes and the
// processes block of /api/runtime/observation each build a fresh discovery
// reader, so the previous-sample cache must live at package scope for
// cpuPercent to be a live Δticks/Δwall rate between polls (#1190).
var defaultProcessCPUSampler = NewProcessCPUSampler(100, nil)

// readProcessesStats finds managed service processes via /proc.
func readProcessesStats(policy ManagedProcessPolicy) (ProcessesStats, error) {
	return NewProcessDiscovery(procProcessSource{cpu: defaultProcessCPUSampler}, policy).Read()
}

type procProcessSource struct {
	cpu *ProcessCPUSampler
}

func (procProcessSource) PIDs() ([]int, error) {
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(procs))
	for _, proc := range procs {
		if !proc.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(proc.Name())
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func (procProcessSource) SystemUptime() (int64, error) { return readSystemUptime() }

func (procProcessSource) Name(pid int) string { return readProcessName(pid) }

func (procProcessSource) MemoryMB(pid int) int64 { return readProcessMemory(pid) }

func (s procProcessSource) CPUPercent(pid int, uptimeSec int64) float64 {
	stat, ok := readProcessStat(pid)
	if !ok {
		return 0
	}
	return s.cpu.Percent(pid, stat, uptimeSec)
}

func (procProcessSource) UptimeSeconds(pid int, systemUptime int64) int64 {
	return readProcessUptime(pid, systemUptime)
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
	return NewProcessMemoryParser().Parse(string(data))
}

func readProcessUptime(pid int, systemUptime int64) int64 {
	stat, ok := readProcessStat(pid)
	if !ok {
		return 0
	}
	return NewProcessUptime(100).Seconds(stat, systemUptime)
}

func readProcessStat(pid int) (ProcessStatFields, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ProcessStatFields{}, false
	}
	return NewProcessStatParser().Parse(string(data))
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
