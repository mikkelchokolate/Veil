package runtime

import (
	"errors"
	"testing"
)

func TestProcessDiscoveryReturnsOnlyManagedProcesses(t *testing.T) {
	source := fakeProcessSource{
		pids:   []int{10, 20, 30},
		names:  map[int]string{10: "caddy", 20: "nginx", 30: "veil"},
		mem:    map[int]int64{10: 64, 30: 32},
		cpu:    map[int]float64{10: 1.5, 30: 2.5},
		uptime: map[int]int64{10: 100, 30: 50},
		system: 200,
	}
	stats, err := NewProcessDiscovery(source).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stats.Processes) != 2 {
		t.Fatalf("processes = %+v", stats.Processes)
	}
	if stats.Processes[0].PID != 10 || stats.Processes[0].Name != "caddy" || stats.Processes[0].MemoryMB != 64 || stats.Processes[0].CPUPercent != 1.5 || stats.Processes[0].UptimeSeconds != 100 {
		t.Fatalf("first process = %+v", stats.Processes[0])
	}
	if stats.Processes[1].PID != 30 || stats.Processes[1].Name != "veil" {
		t.Fatalf("second process = %+v", stats.Processes[1])
	}
}

func TestProcessDiscoveryReturnsPIDListErrors(t *testing.T) {
	_, err := NewProcessDiscovery(fakeProcessSource{err: errors.New("boom")}).Read()
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeProcessSource struct {
	pids   []int
	names  map[int]string
	mem    map[int]int64
	cpu    map[int]float64
	uptime map[int]int64
	system int64
	err    error
}

func (s fakeProcessSource) PIDs() ([]int, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.pids, nil
}

func (s fakeProcessSource) SystemUptime() (int64, error) { return s.system, nil }

func (s fakeProcessSource) Name(pid int) string { return s.names[pid] }

func (s fakeProcessSource) MemoryMB(pid int) int64 { return s.mem[pid] }

func (s fakeProcessSource) CPUPercent(pid int, uptimeSec int64) float64 { return s.cpu[pid] }

func (s fakeProcessSource) UptimeSeconds(pid int, systemUptime int64) int64 { return s.uptime[pid] }
