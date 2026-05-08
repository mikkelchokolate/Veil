package cli

import "runtime"

type DoctorReadiness struct {
	version string
}

func NewDoctorReadiness(version string) DoctorReadiness {
	return DoctorReadiness{version: version}
}

func (d DoctorReadiness) Summary() doctorSummary {
	summary := doctorSummary{
		Version: d.version,
		Runtime: runtime.GOOS + "/" + runtime.GOARCH,
		Ready:   true,
	}
	required := []string{"systemctl"}
	optional := []string{"caddy", "hysteria", "mieru", "sing-box", "ufw"}

	for _, name := range required {
		status := doctorCommandStatus{Name: name}
		path, err := commandLookPath(name)
		if err == nil {
			status.Path = path
			status.Present = true
		} else {
			status.Error = err.Error()
			summary.Ready = false
		}
		summary.Commands = append(summary.Commands, status)
	}
	for _, name := range optional {
		status := doctorCommandStatus{Name: name, Optional: true}
		path, err := commandLookPath(name)
		if err == nil {
			status.Path = path
			status.Present = true
		} else {
			status.Error = err.Error()
		}
		summary.Commands = append(summary.Commands, status)
	}
	return summary
}

func buildDoctorSummary(version string) doctorSummary {
	return NewDoctorReadiness(version).Summary()
}
