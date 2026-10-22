package cli

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	doctorflow "github.com/mikkelchokolate/Veil/internal/cliflow/doctor"
)

// TestDoctorReportsUnreachableProtocolDNS is the #1288 preflight regression:
// on a host whose nameservers are all denied by the protocol units' egress
// filter, `veil doctor` must say so (and that Mieru is left without DNS)
// instead of printing "Ready: yes" and nothing else. It stays a warning.
func TestDoctorReportsUnreachableProtocolDNS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the egress filter and its DNS check exist only on Linux")
	}
	old := doctorEgressDNSProbe
	t.Cleanup(func() { doctorEgressDNSProbe = old })
	doctorEgressDNSProbe = func() doctorflow.EgressDNSCheck {
		return doctorflow.EgressDNSCheck{Nameservers: []string{"172.31.0.2"}, PinnedDoH: "1.1.1.1:443"}
	}

	summary := buildDoctorSummary("test")
	if !summary.Ready {
		t.Fatalf("an unreachable resolver is a warning, not a readiness failure: %+v", summary)
	}
	if summary.EgressDNS == nil || summary.EgressDNS.Reachable || !strings.Contains(summary.EgressDNS.Warning, "Mieru") {
		t.Fatalf("EgressDNS = %+v, want unreachable with a Mieru warning", summary.EgressDNS)
	}

	var out bytes.Buffer
	cmd := NewRootCommand("test")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	for _, want := range []string{
		"Ready: yes",
		"Protocol DNS (egress filter):",
		"- nameservers: 172.31.0.2",
		"- reachable from protocol units: no (warning)",
		"Cloudflare DoH 1.1.1.1:443",
		"- Mieru: domain lookups fail",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	cmd = NewRootCommand("test")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"doctor", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor --json: %v", err)
	}
	var decoded doctorflow.Summary
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if decoded.EgressDNS == nil || decoded.EgressDNS.Reachable || decoded.EgressDNS.PinnedDoH != "1.1.1.1:443" || decoded.EgressDNS.Warning == "" {
		t.Fatalf("JSON egressDNS = %+v", decoded.EgressDNS)
	}
}
