package api

import (
	"fmt"
	"strings"
)

type PingOutputParser struct{}

func NewPingOutputParser() PingOutputParser { return PingOutputParser{} }

func (PingOutputParser) Parse(output string, result *PingResult) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "packets transmitted") {
			fmt.Sscanf(line, "%d packets transmitted, %d received", &result.Transmitted, &result.Received)
		}
		if strings.Contains(line, "min/avg/max") || strings.Contains(line, "rtt min/avg/max") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				stats := strings.Fields(strings.TrimSpace(parts[1]))
				if len(stats) >= 1 {
					times := strings.Split(stats[0], "/")
					if len(times) >= 4 {
						fmt.Sscanf(times[0], "%f", &result.MinMs)
						fmt.Sscanf(times[1], "%f", &result.AvgMs)
						fmt.Sscanf(times[2], "%f", &result.MaxMs)
						fmt.Sscanf(times[3], "%f", &result.StddevMs)
					}
				}
			}
		}
	}
}

func parsePingOutput(output string, result *PingResult) {
	NewPingOutputParser().Parse(output, result)
}
