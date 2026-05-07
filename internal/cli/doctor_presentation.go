package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

type DoctorPresentation struct {
	out io.Writer
}

func NewDoctorPresentation(out io.Writer) DoctorPresentation {
	return DoctorPresentation{out: out}
}

func (p DoctorPresentation) Render(summary doctorSummary, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(p.out).Encode(summary)
	}
	fmt.Fprintln(p.out, "Veil doctor")
	fmt.Fprintf(p.out, "Version: %s\n", summary.Version)
	fmt.Fprintf(p.out, "Runtime: %s\n", summary.Runtime)
	if summary.Ready {
		fmt.Fprintln(p.out, "Ready: yes")
	} else {
		fmt.Fprintln(p.out, "Ready: no")
	}
	fmt.Fprintln(p.out, "Required commands:")
	for _, command := range summary.Commands {
		if command.Optional {
			continue
		}
		if !command.Present {
			if command.Error != "" {
				fmt.Fprintf(p.out, "- %s: missing (%s)\n", command.Name, command.Error)
				continue
			}
			fmt.Fprintf(p.out, "- %s: missing\n", command.Name)
			continue
		}
		fmt.Fprintf(p.out, "- %s: %s\n", command.Name, command.Path)
	}
	fmt.Fprintln(p.out, "Optional commands:")
	hasOptional := false
	for _, command := range summary.Commands {
		if !command.Optional {
			continue
		}
		hasOptional = true
		if !command.Present {
			fmt.Fprintf(p.out, "- %s: missing (warning)\n", command.Name)
			continue
		}
		fmt.Fprintf(p.out, "- %s: %s\n", command.Name, command.Path)
	}
	if !hasOptional {
		fmt.Fprintln(p.out, "- none")
	}
	return nil
}
