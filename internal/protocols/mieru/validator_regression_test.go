package mieru

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidateInboundDoesNotRejectRuntimeSupportedPrivilegedPorts(t *testing.T) {
	plugin := New()
	for _, port := range []int{1, 80, 443, 1024, 1025, 65535} {
		issues := plugin.ValidateInbound(model.Settings{}, model.Inbound{Protocol: "mieru", Transport: "tcp", Port: port})
		if len(issues) != 0 {
			t.Errorf("port %d rejected by Mieru-specific validation: %+v; pinned mita v3.34.1 accepts 1..65535", port, issues)
		}
	}
}
