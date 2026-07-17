package naiveproxy

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidationAcceptsEnabledClientProfileCredentials(t *testing.T) {
	plugin := New()
	settings := model.Settings{
		Domain: "vpn.example.com",
		Email:  "admin@example.com",
	}
	inbound := model.Inbound{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		ProtocolFields: map[string]any{
			"domain": "vpn.example.com",
			"email":  "admin@example.com",
		},
		Profiles: []model.ClientProfile{{
			Name:     "alice",
			Username: "alice",
			Password: "secret",
			Enabled:  true,
		}},
	}

	if err := plugin.ValidateSettings(settings, inbound); err != nil {
		t.Fatalf("ValidateSettings rejected profile credentials: %v", err)
	}
	if issues := plugin.ValidateInbound(settings, inbound); len(issues) != 0 {
		t.Fatalf("ValidateInbound rejected profile credentials: %+v", issues)
	}
}

func TestValidationRejectsIncompleteClientProfileWithoutFallback(t *testing.T) {
	plugin := New()
	settings := model.Settings{
		Domain: "vpn.example.com",
		Email:  "admin@example.com",
	}
	inbound := model.Inbound{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		ProtocolFields: map[string]any{
			"domain": "vpn.example.com",
			"email":  "admin@example.com",
		},
		Profiles: []model.ClientProfile{{
			Name:    "alice",
			Enabled: true,
		}},
	}

	issues := plugin.ValidateInbound(settings, inbound)
	if len(issues) != 1 || issues[0].Code != "naive_credential_required" {
		t.Fatalf("ValidateInbound issues = %+v, want naive_credential_required", issues)
	}
}
