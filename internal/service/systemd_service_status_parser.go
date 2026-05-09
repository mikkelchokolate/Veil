package service

import "strings"

type SystemdServiceStatusParser struct{}

func NewSystemdServiceStatusParser() SystemdServiceStatusParser { return SystemdServiceStatusParser{} }

func (SystemdServiceStatusParser) Parse(unit string, output string) ServiceRuntimeStatus {
	status := ServiceRuntimeStatus{Unit: unit, LoadState: "unknown", ActiveState: "unknown", SubState: "unknown"}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			if value != "" {
				status.LoadState = value
			}
		case "ActiveState":
			if value != "" {
				status.ActiveState = value
			}
		case "SubState":
			if value != "" {
				status.SubState = value
			}
		}
	}
	return status
}
