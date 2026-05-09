package runtime

import "testing"

func TestProcessUptimeCalculatesElapsedSecondsFromStartTicks(t *testing.T) {
	uptime := NewProcessUptime(100)
	got := uptime.Seconds(ProcessStatFields{StartTimeTicks: 250}, 10)
	if got != 8 {
		t.Fatalf("uptime = %d", got)
	}
}

func TestProcessUptimeDefaultsInvalidClockTicks(t *testing.T) {
	got := NewProcessUptime(0).Seconds(ProcessStatFields{StartTimeTicks: 100}, 10)
	if got != 9 {
		t.Fatalf("uptime = %d", got)
	}
}
