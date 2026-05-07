package api

import "testing"

func TestMetricsServiceStatusSetsActiveAndInactiveGaugeValues(t *testing.T) {
	collector := NewMetricsCollector()
	status := NewMetricsServiceStatus(collector)
	status.Set("caddy", true)
	status.Set("hysteria2", false)

	collector.statusMu.RLock()
	defer collector.statusMu.RUnlock()
	if collector.serviceStatus["caddy"] != 1 || collector.serviceStatus["hysteria2"] != 0 {
		t.Fatalf("serviceStatus = %+v", collector.serviceStatus)
	}
}
