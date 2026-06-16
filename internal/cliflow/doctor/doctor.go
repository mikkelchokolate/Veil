package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
)

type LookupFunc func(string) (string, error)

type Summary struct {
	Version  string          `json:"version"`
	Runtime  string          `json:"runtime"`
	Ready    bool            `json:"ready"`
	Commands []CommandStatus `json:"commands"`
}

type CommandStatus struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
	Present  bool   `json:"present"`
	Optional bool   `json:"optional,omitempty"`
}

type Readiness struct {
	version string
	lookup  LookupFunc
}

func NewReadiness(version string, lookup LookupFunc) Readiness {
	return Readiness{version: version, lookup: lookup}
}

func (d Readiness) Summary() Summary {
	summary := Summary{
		Version: d.version,
		Runtime: runtime.GOOS + "/" + runtime.GOARCH,
		Ready:   true,
	}
	required := []string{"systemctl"}
	optional := []string{"caddy", "hysteria", "mita", "olcrtc", "sing-box", "ufw"}
	for _, name := range required {
		status := CommandStatus{Name: name}
		path, err := d.lookup(name)
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
		status := CommandStatus{Name: name, Optional: true}
		path, err := d.lookup(name)
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

type Presentation struct {
	out io.Writer
}

func NewPresentation(out io.Writer) Presentation {
	return Presentation{out: out}
}

func (p Presentation) Render(summary Summary, jsonOutput bool) error {
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
