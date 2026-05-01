package installer

import "fmt"

type PortAvailability struct {
	TCPBusy map[int]bool
	UDPBusy map[int]bool
}

type EndpointPlan struct {
	Port      int
	Transport string
}

type SharedPortPlan struct {
	Port      int
	Naive     EndpointPlan
	Hysteria2 EndpointPlan
	Changed   bool
	Random    bool
	Reason    string
}

func PlanSharedPort(availability PortAvailability, preferred []int, randomPort func() int) SharedPortPlan {
	if len(preferred) == 0 {
		preferred = []int{443, 8443}
	}
	original := preferred[0]
	for _, port := range preferred {
		if !availability.TCPBusy[port] && !availability.UDPBusy[port] {
			return sharedPlan(port, port != original, false, reason(original, port, false))
		}
	}
	port := randomPort()
	return sharedPlan(port, true, true, reason(original, port, true))
}

func PlanStackPort(availability PortAvailability, preferred []int, randomPort func() int, needTCP, needUDP bool) SharedPortPlan {
	if needTCP && needUDP {
		return PlanSharedPort(availability, preferred, randomPort)
	}
	if len(preferred) == 0 {
		preferred = []int{443, 8443}
	}
	original := preferred[0]
	for _, port := range preferred {
		if (!needTCP || !availability.TCPBusy[port]) && (!needUDP || !availability.UDPBusy[port]) {
			return sharedPlan(port, port != original, false, reason(original, port, false))
		}
	}
	port := randomPort()
	return sharedPlan(port, true, true, reason(original, port, true))
}

func PlanExplicitStackPort(availability PortAvailability, port int, needTCP, needUDP bool) (SharedPortPlan, error) {
	if port <= 0 || port > 65535 {
		return SharedPortPlan{}, fmt.Errorf("invalid shared proxy port %d", port)
	}
	if needTCP && availability.TCPBusy[port] {
		return SharedPortPlan{}, fmt.Errorf("shared proxy port %d/tcp is already in use", port)
	}
	if needUDP && availability.UDPBusy[port] {
		return SharedPortPlan{}, fmt.Errorf("shared proxy port %d/udp is already in use", port)
	}
	return sharedPlan(port, false, false, "user selected shared proxy port"), nil
}

func sharedPlan(port int, changed bool, random bool, why string) SharedPortPlan {
	return SharedPortPlan{
		Port:      port,
		Naive:     EndpointPlan{Port: port, Transport: "tcp"},
		Hysteria2: EndpointPlan{Port: port, Transport: "udp"},
		Changed:   changed,
		Random:    random,
		Reason:    why,
	}
}

func reason(original, selected int, random bool) string {
	if original == selected {
		return ""
	}
	if random {
		return fmt.Sprintf("preferred ports are busy; selected random shared TCP/UDP port %d", selected)
	}
	return fmt.Sprintf("preferred port %d is busy; selected fallback shared TCP/UDP port %d", original, selected)
}
