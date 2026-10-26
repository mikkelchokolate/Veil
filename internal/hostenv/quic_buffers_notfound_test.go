package hostenv

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// TestApplyQUICUDPBuffersNamesTheTrustedSearchPath keeps the failure actionable:
// a host whose sysctl lives outside the trusted directories gets the search path
// in the error instead of a bare "not found" (#1506 review).
func TestApplyQUICUDPBuffersNamesTheTrustedSearchPath(t *testing.T) {
	newQUICFloorFixture(t, map[string]string{
		quicRMEMSysctlPath: "212992",
		quicWMEMSysctlPath: "212992",
	})
	original := quicLookPath
	quicLookPath = func(string) (string, error) { return "", syspath.ErrNotFound }
	t.Cleanup(func() { quicLookPath = original })

	err := ApplyQUICUDPBuffers()
	if err == nil {
		t.Fatal("expected a missing-sysctl error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "sysctl not found for live QUIC buffer tuning") {
		t.Fatalf("error lost the context: %v", err)
	}
	if !strings.Contains(msg, syspath.TrustedPATH()) {
		t.Fatalf("error does not name the trusted search path %q: %v", syspath.TrustedPATH(), err)
	}
}
