package doctor

import (
	"bytes"
	"strings"
	"testing"
)

// TestSummaryReportsUnitKeyPathDrift locks the #1473 doctor check: stale
// pre-#1472 drop-ins that pin the key path are surfaced as a warning without
// flipping Ready, because the host works until the key path moves.
func TestSummaryReportsUnitKeyPathDrift(t *testing.T) {
	lookup := func(string) (string, error) { return "/usr/bin/tool", nil }
	summary := NewReadiness("test", lookup).
		WithUnitKeyPathDrift(func() []string { return []string{"/etc/systemd/system/veil-helper.service.d/10-veil-install.conf"} }).
		Summary()
	if len(summary.UnitKeyPathDrift) != 1 {
		t.Fatalf("UnitKeyPathDrift = %v", summary.UnitKeyPathDrift)
	}
	if !summary.Ready {
		t.Fatal("a stale key pin must not flip Ready before the key path moves")
	}

	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"Managed unit key path (warning)",
		"pins the state key path instead of reading veil.env",
		"veil repair",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("presentation missing %q:\n%s", want, text)
		}
	}
}

// TestSummaryWithoutDriftProbeOmitsTheCheck keeps the default output unchanged
// when no probe is wired.
func TestSummaryWithoutDriftProbeOmitsTheCheck(t *testing.T) {
	lookup := func(string) (string, error) { return "/usr/bin/tool", nil }
	if summary := NewReadiness("test", lookup).Summary(); len(summary.UnitKeyPathDrift) != 0 {
		t.Fatalf("unexpected drift without a probe: %v", summary.UnitKeyPathDrift)
	}
}
