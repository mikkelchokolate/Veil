package mieru

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// The plugin-level validator intentionally owns no port constraint: pinned
// mita v3.36.1 accepts the full 1..65535 range in FlatPortBindings (including
// privileged ports — the systemd unit carries CAP_NET_BIND_SERVICE), and the
// generic [1,65535] range gate lives in the common inbound validator
// (internal/inbounds.TestInboundValidationRejectsPortsAbove65535 and
// TestInboundValidationAcceptsMieruPrivilegedPorts). This test documents that
// delegation instead of pretending to lock the range here (#821).
func TestValidateInboundDelegatesPortRangeToCommonValidator(t *testing.T) {
	plugin := New()
	for _, port := range []int{1, 443, 65535} {
		if issues := plugin.ValidateInbound(model.Settings{}, model.Inbound{Protocol: "mieru", Transport: "tcp", Port: port}); len(issues) != 0 {
			t.Errorf("port %d rejected by Mieru-specific validation: %+v; range gating is the common validator's job", port, issues)
		}
	}
}
