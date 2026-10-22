package cli

import (
	"bytes"
	"reflect"
	"runtime"
	"strings"
	"testing"

	doctorflow "github.com/mikkelchokolate/Veil/internal/cliflow/doctor"
)

// TestParseMieruUnitFallbackState covers the `systemctl show` shapes doctor
// sees: the current unit (with or without systemd's bind options), a unit
// that predates the hook, and a unit that is not loaded at all.
func TestParseMieruUnitFallbackState(t *testing.T) {
	for name, tc := range map[string]struct {
		show string
		want mieruUnitFallbackState
	}{
		"current unit":             {"LoadState=loaded\nBindReadOnlyPaths=-/run/veil-mieru/resolv.conf:/etc/resolv.conf:rbind\n", mieruUnitFallbackInstalled},
		"current unit, no options": {"BindReadOnlyPaths=-/run/veil-mieru/resolv.conf:/etc/resolv.conf\nLoadState=loaded\n", mieruUnitFallbackInstalled},
		"unit predates the hook":   {"LoadState=loaded\nBindReadOnlyPaths=\n", mieruUnitFallbackMissing},
		"other bind only":          {"LoadState=loaded\nBindReadOnlyPaths=/srv/x:/etc/resolv.conf\n", mieruUnitFallbackMissing},
		"unit not installed":       {"LoadState=not-found\nBindReadOnlyPaths=\n", mieruUnitFallbackUnknown},
		"empty output":             {"", mieruUnitFallbackUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			if got := parseMieruUnitFallbackState(tc.show); got != tc.want {
				t.Fatalf("state = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDoctorDoesNotPromiseMieruFallbackOnOutdatedUnit is the review
// regression for #1362: `veil update` does not re-render install-managed
// units, so on a host whose loaded veil-mieru.service lacks the hook doctor
// must keep "domain lookups fail" and point at `veil repair`.
func TestDoctorDoesNotPromiseMieruFallbackOnOutdatedUnit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the egress filter and its DNS check exist only on Linux")
	}
	denied := doctorflow.EgressDNSCheck{Nameservers: []string{"172.31.0.2"}, PinnedDoH: "1.1.1.1:443", MieruNameservers: []string{"1.1.1.1", "1.0.0.1"}}
	oldDNS, oldUnit := doctorEgressDNSProbe, doctorMieruUnitFallbackProbe
	t.Cleanup(func() { doctorEgressDNSProbe, doctorMieruUnitFallbackProbe = oldDNS, oldUnit })
	doctorEgressDNSProbe = func() doctorflow.EgressDNSCheck { return denied }

	for name, tc := range map[string]struct {
		state       mieruUnitFallbackState
		wantOut     string
		wantWarning string
		outdated    bool
	}{
		"installed": {mieruUnitFallbackInstalled, "- Mieru: public fallback resolv.conf (1.1.1.1, 1.0.0.1) at each start", "veil-mieru resolves through 1.1.1.1, 1.0.0.1", false},
		"unknown":   {mieruUnitFallbackUnknown, "- Mieru: public fallback resolv.conf (1.1.1.1, 1.0.0.1) at each start", "veil-mieru resolves through 1.1.1.1, 1.0.0.1", false},
		"outdated":  {mieruUnitFallbackMissing, "- Mieru: domain lookups fail (veil-mieru.service predates the DNS fallback; run `veil repair`)", "run `veil repair`", true},
	} {
		t.Run(name, func(t *testing.T) {
			doctorMieruUnitFallbackProbe = func() mieruUnitFallbackState { return tc.state }
			summary := buildDoctorSummary("test")
			if summary.EgressDNS == nil || summary.EgressDNS.MieruUnitOutdated != tc.outdated {
				t.Fatalf("EgressDNS = %+v, want MieruUnitOutdated=%v", summary.EgressDNS, tc.outdated)
			}
			if !strings.Contains(summary.EgressDNS.Warning, tc.wantWarning) {
				t.Fatalf("warning %q missing %q", summary.EgressDNS.Warning, tc.wantWarning)
			}
			if tc.outdated && len(summary.EgressDNS.MieruNameservers) != 0 {
				t.Fatalf("an outdated unit must not report fallback nameservers: %+v", summary.EgressDNS)
			}
			var out bytes.Buffer
			cmd := NewRootCommand("test")
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"doctor"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("doctor: %v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), tc.wantOut) {
				t.Fatalf("doctor output missing %q:\n%s", tc.wantOut, out.String())
			}
		})
	}

	// install and repair re-render the unit, so they keep the probe's
	// fallback as is: only doctor consults the loaded unit.
	doctorMieruUnitFallbackProbe = func() mieruUnitFallbackState {
		t.Fatal("repair must not consult the loaded unit")
		return mieruUnitFallbackUnknown
	}
	if got := repairEgressDNSWarning(); !strings.Contains(got, "veil-mieru resolves through 1.1.1.1, 1.0.0.1") {
		t.Fatalf("repair warning = %q", got)
	}
}

func TestWithMieruUnitFallbackStateLeavesOtherChecksAlone(t *testing.T) {
	for _, check := range []doctorflow.EgressDNSCheck{
		{Nameservers: []string{"127.0.0.53"}, Reachable: true},
		{ResolvConfMissing: true, PinnedDoH: "1.1.1.1:443"},
		{ReadError: "permission denied"},
	} {
		if got := withMieruUnitFallbackState(check, mieruUnitFallbackMissing); !reflect.DeepEqual(got, check) {
			t.Fatalf("withMieruUnitFallbackState(%+v) = %+v, want unchanged", check, got)
		}
	}
}
