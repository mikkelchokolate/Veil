package api

type MetricsServiceStatus struct {
	collector *MetricsCollector
}

func NewMetricsServiceStatus(collector *MetricsCollector) MetricsServiceStatus {
	return MetricsServiceStatus{collector: collector}
}

func (s MetricsServiceStatus) Set(name string, active bool) {
	s.collector.statusMu.Lock()
	defer s.collector.statusMu.Unlock()
	if s.collector.serviceStatus == nil {
		s.collector.serviceStatus = make(map[string]float64)
	}
	if active {
		s.collector.serviceStatus[name] = 1
		return
	}
	s.collector.serviceStatus[name] = 0
}
