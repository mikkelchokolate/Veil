package cli

import (
	"io"

	doctorflow "github.com/veil-panel/veil/internal/cliflow/doctor"
)

type DoctorPresentation struct {
	inner doctorflow.Presentation
}

func NewDoctorPresentation(out io.Writer) DoctorPresentation {
	return DoctorPresentation{inner: doctorflow.NewPresentation(out)}
}

func (p DoctorPresentation) Render(summary doctorSummary, jsonOutput bool) error {
	return p.inner.Render(summary, jsonOutput)
}
