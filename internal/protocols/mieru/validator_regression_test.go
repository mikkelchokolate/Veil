package mieru

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidateInboundEnforcesUpstreamServerPortRange(t *testing.T) {
	plugin := New()
	for _, tc := range []struct {
		port      int
		wantError bool
	}{
		{port: 1, wantError: true},
		{port: 1024, wantError: true},
		{port: 1025, wantError: false},
		{port: 65535, wantError: false},
		{port: 65536, wantError: true},
	} {
		issues := plugin.ValidateInbound(model.Settings{}, model.Inbound{Protocol: "mieru", Transport: "tcp", Port: tc.port})
		if tc.wantError && len(issues) == 0 {
			t.Errorf("port %d accepted; upstream mita requires 1025..65535", tc.port)
		}
		if !tc.wantError && len(issues) != 0 {
			t.Errorf("port %d rejected: %+v", tc.port, issues)
		}
		if len(issues) > 0 && issues[0].Code != "mieru_port_invalid" {
			t.Errorf("port %d issue code = %q, want mieru_port_invalid", tc.port, issues[0].Code)
		}
	}
}
