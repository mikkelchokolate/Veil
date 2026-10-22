package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"

	doctorflow "github.com/mikkelchokolate/Veil/internal/cliflow/doctor"
	"github.com/mikkelchokolate/Veil/internal/renderer"
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

// TestEgressDNSCheckFromReportMapsEveryField covers the production adapter,
// which TestMain replaces for every other test: a dropped field would
// otherwise print "pinned to Cloudflare DoH  at apply" with a green suite.
func TestEgressDNSCheckFromReportMapsEveryField(t *testing.T) {
	cases := []struct {
		name   string
		report renderer.EgressDNSReport
		want   doctorflow.EgressDNSCheck
	}{
		{
			name:   "unreachable",
			report: renderer.EgressDNSReport{Nameservers: []string{"172.31.0.2", "fe80::1%eth0"}, PinnedDoH: "[2606:4700:4700::1111]:443", MieruNameservers: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}},
			want:   doctorflow.EgressDNSCheck{Nameservers: []string{"172.31.0.2", "fe80::1%eth0"}, PinnedDoH: "[2606:4700:4700::1111]:443", MieruNameservers: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}},
		},
		{
			name:   "reachable",
			report: renderer.EgressDNSReport{Nameservers: []string{"127.0.0.53"}, Reachable: true},
			want:   doctorflow.EgressDNSCheck{Nameservers: []string{"127.0.0.53"}, Reachable: true},
		},
		{
			name:   "missing",
			report: renderer.EgressDNSReport{ResolvConfMissing: true, PinnedDoH: "1.1.1.1:443"},
			want:   doctorflow.EgressDNSCheck{Nameservers: []string{}, ResolvConfMissing: true, PinnedDoH: "1.1.1.1:443"},
		},
		{
			name:   "unreadable",
			report: renderer.EgressDNSReport{ReadError: "permission denied"},
			want:   doctorflow.EgressDNSCheck{Nameservers: []string{}, ReadError: "permission denied"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := egressDNSCheckFromReport(tc.report)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("egressDNSCheckFromReport(%+v) = %+v, want %+v", tc.report, got, tc.want)
			}
		})
	}
	// Every exported report field must be consumed: a new field added to the
	// renderer report without a doctor mapping fails here.
	if n := reflect.TypeOf(renderer.EgressDNSReport{}).NumField(); n != 6 {
		t.Fatalf("renderer.EgressDNSReport has %d fields; update egressDNSCheckFromReport and this test", n)
	}
}

// TestDoctorJSONEmitsEmptyNameserverList: a missing resolv.conf must
// serialize as "nameservers":[] rather than null.
func TestDoctorJSONEmitsEmptyNameserverList(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the egress filter and its DNS check exist only on Linux")
	}
	old := doctorEgressDNSProbe
	t.Cleanup(func() { doctorEgressDNSProbe = old })
	doctorEgressDNSProbe = func() doctorflow.EgressDNSCheck {
		return doctorflow.EgressDNSCheck{ResolvConfMissing: true, PinnedDoH: "1.1.1.1:443"}
	}
	var out bytes.Buffer
	cmd := NewRootCommand("test")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"doctor", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor --json: %v", err)
	}
	if !strings.Contains(out.String(), `"nameservers":[]`) {
		t.Fatalf("want \"nameservers\":[] in JSON, got:\n%s", out.String())
	}
}
