package doctor

import (
	"bytes"
	"strings"
	"testing"
)

func presentHost(name string) (string, error) { return "/usr/bin/" + name, nil }

// TestEgressDNSCheckIsOptional: without a probe (non-Linux) the summary has
// no DNS section, so existing output and JSON stay unchanged.
func TestEgressDNSCheckIsOptional(t *testing.T) {
	summary := NewReadiness("test", presentHost).Summary()
	if summary.EgressDNS != nil {
		t.Fatalf("EgressDNS = %+v without a probe", summary.EgressDNS)
	}
	var out bytes.Buffer
	_ = NewPresentation(&out).Render(summary, false)
	if strings.Contains(out.String(), "Protocol DNS") {
		t.Fatalf("DNS section rendered without a probe:\n%s", out.String())
	}
}

// TestEgressDNSCheckStates covers the #1288 doctor states: reachable (no
// warning), unreachable (warning naming Mieru and the pinned DoH), missing
// resolv.conf (unreachable), and unreadable resolv.conf (unknown). None of
// them flips Ready.
func TestEgressDNSCheckStates(t *testing.T) {
	cases := []struct {
		name        string
		check       EgressDNSCheck
		wantWarning []string
		wantOut     []string
		notOut      []string
	}{
		{
			name:    "reachable",
			check:   EgressDNSCheck{Nameservers: []string{"127.0.0.53"}, Reachable: true},
			wantOut: []string{"- nameservers: 127.0.0.53", "- reachable from protocol units: yes"},
			notOut:  []string{"Warning:", "Mieru"},
		},
		{
			name:        "unreachable",
			check:       EgressDNSCheck{Nameservers: []string{"10.0.0.2", "192.168.1.1"}, PinnedDoH: "[2606:4700:4700::1111]:443"},
			wantWarning: []string{"Mieru", "[2606:4700:4700::1111]:443", "systemd-resolved", "#1288"},
			wantOut:     []string{"- nameservers: 10.0.0.2, 192.168.1.1", "- reachable from protocol units: no (warning)", "- Mieru: domain lookups fail", "Warning: "},
		},
		{
			name:        "unreachable with Mieru fallback",
			check:       EgressDNSCheck{Nameservers: []string{"172.31.0.2"}, PinnedDoH: "1.1.1.1:443", MieruNameservers: []string{"1.1.1.1", "1.0.0.1"}},
			wantWarning: []string{"veil-mieru resolves through 1.1.1.1, 1.0.0.1", "plain DNS", "1.1.1.1:443", "#1288"},
			wantOut:     []string{"- reachable from protocol units: no (warning)", "- Mieru: public fallback resolv.conf (1.1.1.1, 1.0.0.1) at each start", "Warning: "},
			notOut:      []string{"domain lookups fail"},
		},
		{
			name:        "missing resolv.conf",
			check:       EgressDNSCheck{ResolvConfMissing: true, PinnedDoH: "1.1.1.1:443"},
			wantWarning: []string{"Mieru"},
			wantOut:     []string{"- resolv.conf: missing", "- reachable from protocol units: no (warning)"},
		},
		{
			name:        "empty resolv.conf",
			check:       EgressDNSCheck{PinnedDoH: "1.1.1.1:443"},
			wantWarning: []string{"Mieru"},
			wantOut:     []string{"- resolv.conf: no nameservers"},
		},
		{
			name:        "unreadable resolv.conf",
			check:       EgressDNSCheck{ReadError: "permission denied"},
			wantWarning: []string{"cannot read /etc/resolv.conf (permission denied)"},
			wantOut:     []string{"- resolv.conf: unreadable (warning)"},
			notOut:      []string{"reachable from protocol units"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := tc.check
			summary := NewReadiness("test", presentHost).WithEgressDNS(func() EgressDNSCheck { return check }).Summary()
			if !summary.Ready {
				t.Fatal("the DNS check must never flip Ready")
			}
			got := summary.EgressDNS
			if got == nil {
				t.Fatal("EgressDNS missing")
			}
			if len(tc.wantWarning) == 0 && got.Warning != "" {
				t.Fatalf("unexpected warning %q", got.Warning)
			}
			for _, want := range tc.wantWarning {
				if !strings.Contains(got.Warning, want) {
					t.Fatalf("warning %q missing %q", got.Warning, want)
				}
			}
			var out bytes.Buffer
			_ = NewPresentation(&out).Render(summary, false)
			for _, want := range append([]string{"Protocol DNS (egress filter):"}, tc.wantOut...) {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("output missing %q:\n%s", want, out.String())
				}
			}
			for _, not := range tc.notOut {
				if strings.Contains(out.String(), not) {
					t.Fatalf("output unexpectedly contains %q:\n%s", not, out.String())
				}
			}
		})
	}
}
