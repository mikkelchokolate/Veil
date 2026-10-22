package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
)

type LookupFunc func(string) (string, error)

type Summary struct {
	Version  string          `json:"version"`
	Runtime  string          `json:"runtime"`
	Ready    bool            `json:"ready"`
	Commands []CommandStatus `json:"commands"`
	// EgressDNS is the protocol-unit DNS check (#1288). Nil when the
	// platform has no such filter or no probe was configured.
	EgressDNS *EgressDNSCheck `json:"egressDNS,omitempty"`
}

// EgressDNSCheck reports whether the host resolver is reachable from the
// protocol units, which run under a kernel egress filter that denies every
// private, loopback, link-local and CGNAT address except the
// systemd-resolved stubs. The units stay active while every lookup is
// dropped, so this is the only place the outage surfaces before clients
// report it. It is a warning and never flips Ready: Hysteria2 and WARP pin a
// public DoH resolver on such hosts, and only Mieru is left without DNS.
type EgressDNSCheck struct {
	Nameservers       []string `json:"nameservers"`
	Reachable         bool     `json:"reachable"`
	ResolvConfMissing bool     `json:"resolvConfMissing,omitempty"`
	ReadError         string   `json:"readError,omitempty"`
	PinnedDoH         string   `json:"pinnedDoH,omitempty"`
	Warning           string   `json:"warning,omitempty"`
}

// EgressDNSProbe returns the raw check; Summary fills in Warning.
type EgressDNSProbe func() EgressDNSCheck

type CommandStatus struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
	Present  bool   `json:"present"`
	Optional bool   `json:"optional,omitempty"`
}

type Readiness struct {
	version   string
	lookup    LookupFunc
	egressDNS EgressDNSProbe
}

func NewReadiness(version string, lookup LookupFunc) Readiness {
	return Readiness{version: version, lookup: lookup}
}

// WithEgressDNS adds the protocol-unit DNS check to the summary.
func (d Readiness) WithEgressDNS(probe EgressDNSProbe) Readiness {
	d.egressDNS = probe
	return d
}

const (
	egressDNSWarningUnreachable = "no resolv.conf nameserver is reachable under the protocol egress filter: " +
		"Hysteria2 and WARP pin Cloudflare DoH %s at apply, but Mieru domain lookups fail; " +
		"enable systemd-resolved (127.0.0.53) or configure a public nameserver, then re-apply (#1288)"
	egressDNSWarningUnknown = "cannot read /etc/resolv.conf (%s): protocol units keep the host resolver, " +
		"which may be unreachable under their egress filter (#1288)"
)

func egressDNSWarning(check EgressDNSCheck) string {
	switch {
	case check.ReadError != "":
		return fmt.Sprintf(egressDNSWarningUnknown, check.ReadError)
	case !check.Reachable:
		return fmt.Sprintf(egressDNSWarningUnreachable, check.PinnedDoH)
	}
	return ""
}

func (d Readiness) Summary() Summary {
	summary := Summary{
		Version: d.version,
		Runtime: runtime.GOOS + "/" + runtime.GOARCH,
		Ready:   true,
	}
	// The protocol runtime binaries are required, not optional: every managed
	// protocol unit execs one of them, so a missing binary leaves a unit that
	// fails with systemd 203/EXEC — reporting "Ready: yes" in that state was a
	// lie (issue #1027). Only ufw stays optional: local-access installs do not
	// manage a firewall.
	required := []string{"systemctl", "caddy", "hysteria", "mita", "olcrtc", "sing-box"}
	optional := []string{"ufw"}
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
	if d.egressDNS != nil {
		check := d.egressDNS()
		check.Warning = egressDNSWarning(check)
		if check.Nameservers == nil {
			check.Nameservers = []string{}
		}
		summary.EgressDNS = &check
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
	if check := summary.EgressDNS; check != nil {
		p.renderEgressDNS(*check)
	}
	return nil
}

func (p Presentation) renderEgressDNS(check EgressDNSCheck) {
	fmt.Fprintln(p.out, "Protocol DNS (egress filter):")
	switch {
	case check.ReadError != "":
		fmt.Fprintln(p.out, "- resolv.conf: unreadable (warning)")
	case check.ResolvConfMissing:
		fmt.Fprintln(p.out, "- resolv.conf: missing")
	case len(check.Nameservers) == 0:
		fmt.Fprintln(p.out, "- resolv.conf: no nameservers")
	default:
		fmt.Fprintf(p.out, "- nameservers: %s\n", strings.Join(check.Nameservers, ", "))
	}
	if check.ReadError == "" {
		if check.Reachable {
			fmt.Fprintln(p.out, "- reachable from protocol units: yes")
		} else {
			fmt.Fprintln(p.out, "- reachable from protocol units: no (warning)")
			fmt.Fprintf(p.out, "- Hysteria2, WARP: pinned to Cloudflare DoH %s at apply\n", check.PinnedDoH)
			fmt.Fprintln(p.out, "- Mieru: domain lookups fail")
		}
	}
	if check.Warning != "" {
		fmt.Fprintf(p.out, "Warning: %s\n", check.Warning)
	}
}
