package cli

import doctorflow "github.com/veil-panel/veil/internal/cliflow/doctor"

type DoctorReadiness struct {
	inner doctorflow.Readiness
}

func NewDoctorReadiness(version string) DoctorReadiness {
	return DoctorReadiness{inner: doctorflow.NewReadiness(version, commandLookPath)}
}

func (d DoctorReadiness) Summary() doctorSummary {
	return d.inner.Summary()
}

func buildDoctorSummary(version string) doctorSummary {
	return NewDoctorReadiness(version).Summary()
}
