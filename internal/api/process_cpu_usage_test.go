package api

import "testing"

func TestProcessCPUUsageCalculatesPercentFromTicksAndStartTime(t *testing.T) {
	usage := NewProcessCPUUsage(100)
	got := usage.Percent(ProcessStatFields{UserTicks: 100, SystemTicks: 50, StartTimeTicks: 1000}, 20)
	if got != 15 {
		t.Fatalf("percent = %v", got)
	}
}

func TestProcessCPUUsageReturnsZeroWhenSystemUptimeOrElapsedIsInvalid(t *testing.T) {
	usage := NewProcessCPUUsage(100)
	if got := usage.Percent(ProcessStatFields{UserTicks: 100, SystemTicks: 50, StartTimeTicks: 1000}, 0); got != 0 {
		t.Fatalf("zero uptime percent = %v", got)
	}
	if got := usage.Percent(ProcessStatFields{UserTicks: 100, SystemTicks: 50, StartTimeTicks: 3000}, 20); got != 0 {
		t.Fatalf("negative elapsed percent = %v", got)
	}
}
