package panelaccess

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestProtocolStringBranches(t *testing.T) {
	for _, tc := range []struct {
		name     string
		m        map[string]any
		key      string
		fallback string
		want     string
	}{
		{"nil map", nil, "key", "fallback", "fallback"},
		{"key missing", map[string]any{"other": "value"}, "key", "fallback", "fallback"},
		{"value not string", map[string]any{"key": 123}, "key", "fallback", "fallback"},
		{"value trimmed", map[string]any{"key": "  spaced  "}, "key", "fallback", "spaced"},
		{"value exact", map[string]any{"key": "exact"}, "key", "fallback", "exact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := protocolString(tc.m, tc.key, tc.fallback); got != tc.want {
				t.Fatalf("protocolString(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNaiveCaddySettingsRequirementProtocolFields(t *testing.T) {
	req := NewNaiveCaddySettingsRequirement()

	// ProtocolFields override the legacy settings fields.
	err := req.Validate(model.Settings{
		Domain:        "vpn.example.com",
		Email:         "admin@example.com",
		NaiveUsername: "legacy-user",
		NaivePassword: "legacy-pass",
		ProtocolFields: map[string]any{
			"naiveUsername": "  protocol-user  ",
			"naivePassword": "protocol-pass",
		},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Missing password in ProtocolFields falls back to settings.
	err = req.Validate(model.Settings{
		Domain:        "vpn.example.com",
		Email:         "admin@example.com",
		NaiveUsername: "legacy-user",
		NaivePassword: "legacy-pass",
		ProtocolFields: map[string]any{
			"naiveUsername": "protocol-user",
		},
	})
	if err != nil {
		t.Fatalf("Validate with fallback password: %v", err)
	}

	// Non-string ProtocolFields value falls back to settings.
	err = req.Validate(model.Settings{
		Domain:        "vpn.example.com",
		Email:         "admin@example.com",
		NaiveUsername: "legacy-user",
		NaivePassword: "legacy-pass",
		ProtocolFields: map[string]any{
			"naiveUsername": 123,
			"naivePassword": 456,
		},
	})
	if err != nil {
		t.Fatalf("Validate with non-string ProtocolFields: %v", err)
	}
}

func TestCaddyRequirementNilFunc(t *testing.T) {
	req := NewCaddyRequirement(nil)
	if req.Required(model.Settings{}, []model.Inbound{{Protocol: "naiveproxy", Enabled: true}}) {
		t.Fatal("nil requiresCaddy should not require Caddy")
	}
}

func TestCaddyRequirementDisabledInbound(t *testing.T) {
	req := NewCaddyRequirement(func(protocol string) bool { return protocol == "naiveproxy" })
	if req.Required(model.Settings{}, []model.Inbound{{Protocol: "naiveproxy", Enabled: false}}) {
		t.Fatal("disabled inbound should not require Caddy")
	}
}

func TestErrNaiveCaddySettingsRequired(t *testing.T) {
	var err errNaiveCaddySettingsRequired
	if !strings.Contains(err.Error(), "domain, email, naive username, and naive password") {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}
