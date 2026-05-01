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

func sharedPlan(port int, changed bool, random bool, why string) SharedPortPlan {
	return SharedPortPlan{
		Port: port,
		Naive: EndpointPlan{Port: port, Transport: "tcp"},
		Hysteria2: EndpointPlan{Port: port, Transport: "udp"},
		Changed: changed,
		Random: random,
		Reason: why,
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
