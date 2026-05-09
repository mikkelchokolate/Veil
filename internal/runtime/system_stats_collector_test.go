package runtime

import (
	"errors"
	"testing"
	"time"
)

func TestSystemStatsCollectorBuildsStatsFromSource(t *testing.T) {
	source := fakeSystemStatsSource{
		mem:        memInfo{total: 2048 * 1024, used: 512 * 1024},
		disk:       diskInfo{total: 10 * 1024 * 1024 * 1024, used: 3 * 1024 * 1024 * 1024},
		uptime:     123,
		load:       loadAvg{avg1: 1.1, avg5: 2.2, avg15: 3.3},
		cpuSamples: [][2]uint64{{40, 100}, {50, 200}},
	}
	stats, err := NewSystemStatsCollector(&source).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if stats.MemoryTotalMB != 2048 || stats.MemoryUsedMB != 512 {
		t.Fatalf("memory stats = %+v", stats)
	}
	if stats.DiskTotalGB != 10 || stats.DiskUsedGB != 3 {
		t.Fatalf("disk stats = %+v", stats)
	}
	if stats.UptimeSeconds != 123 || stats.LoadAvg1 != 1.1 || stats.LoadAvg5 != 2.2 || stats.LoadAvg15 != 3.3 {
		t.Fatalf("runtime stats = %+v", stats)
	}
	if stats.CPUPercent != 90 {
		t.Fatalf("cpuPercent = %f", stats.CPUPercent)
	}
	if source.sleep != 100*time.Millisecond {
		t.Fatalf("sleep = %v", source.sleep)
	}
}

func TestSystemStatsCollectorIgnoresSourceErrors(t *testing.T) {
	source := fakeSystemStatsSource{err: errors.New("boom")}
	stats, err := NewSystemStatsCollector(&source).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if stats != (SystemStats{}) {
		t.Fatalf("stats = %+v", stats)
	}
}

type fakeSystemStatsSource struct {
	mem        memInfo
	disk       diskInfo
	uptime     int64
	load       loadAvg
	cpuSamples [][2]uint64
	sleep      time.Duration
	err        error
}

func (s fakeSystemStatsSource) Meminfo() (memInfo, error) {
	if s.err != nil {
		return memInfo{}, s.err
	}
	return s.mem, nil
}

func (s fakeSystemStatsSource) DiskStats(path string) (diskInfo, error) {
	if s.err != nil {
		return diskInfo{}, s.err
	}
	return s.disk, nil
}

func (s fakeSystemStatsSource) Uptime() (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.uptime, nil
}

func (s fakeSystemStatsSource) LoadAvg() (loadAvg, error) {
	if s.err != nil {
		return loadAvg{}, s.err
	}
	return s.load, nil
}

func (s *fakeSystemStatsSource) CPUTicks() (uint64, uint64) {
	if len(s.cpuSamples) == 0 {
		return 0, 0
	}
	sample := s.cpuSamples[0]
	s.cpuSamples = s.cpuSamples[1:]
	return sample[0], sample[1]
}

func (s *fakeSystemStatsSource) Sleep(duration time.Duration) {
	s.sleep = duration
}
