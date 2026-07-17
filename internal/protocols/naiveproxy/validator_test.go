package naiveproxy

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidateInboundMissingDomain(t *testing.T) {
	v := Validator{}
	issues := v.ValidateInbound(model.Settings{}, model.Inbound{Protocol: "naiveproxy", ProtocolFields: map[string]any{}})
	if len(issues) == 0 {
		t.Fatal("expected missing domain issue")
	}
}

func TestValidateInboundInvalidTransport(t *testing.T) {
	v := Validator{}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		ProtocolFields: map[string]any{
			"domain":    "x.com",
			"transport": "udp",
		},
	}
	issues := v.ValidateInbound(model.Settings{}, inbound)
	found := false
	for _, i := range issues {
		if i.Field == "transport" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected invalid transport issue")
	}
}
