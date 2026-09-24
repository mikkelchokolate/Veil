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

// The earlier version of this test set BOTH the legacy fields and
// ProtocolFields in every case, so err == nil could not prove which source
// validation actually consumed. Each case below isolates one source.
func TestNaiveCaddySettingsRequirementProtocolFields(t *testing.T) {
	req := NewNaiveCaddySettingsRequirement()
	base := model.Settings{Domain: "vpn.example.com", Email: "admin@example.com"}

	// ProtocolFields alone satisfy the requirement when the legacy fields are
	// empty — proving the map is the consulted source.
	withFields := base
	withFields.ProtocolFields = map[string]any{
		"naiveUsername": "  protocol-user  ",
		"naivePassword": "protocol-pass",
	}
	if err := req.Validate(withFields); err != nil {
		t.Fatalf("ProtocolFields should satisfy validation without legacy fields: %v", err)
	}

	// A whitespace-only ProtocolFields value trims to empty and does NOT fall
	// back to a populated legacy field — the key being present wins.
	blanked := base
	blanked.NaiveUsername = "legacy-user"
	blanked.NaivePassword = "legacy-pass"
	blanked.ProtocolFields = map[string]any{"naiveUsername": "   "}
	if err := req.Validate(blanked); err == nil {
		t.Fatal("whitespace ProtocolFields value must blank the credential, not fall back to settings")
	}

	// A missing key falls back to the legacy settings value.
	fallback := base
	fallback.NaivePassword = "legacy-pass"
	fallback.ProtocolFields = map[string]any{"naiveUsername": "protocol-user"}
	if err := req.Validate(fallback); err != nil {
		t.Fatalf("missing ProtocolFields key must fall back to settings: %v", err)
	}

	// Non-string values are ignored, so they fall back to settings...
	nonString := base
	nonString.NaiveUsername = "legacy-user"
	nonString.NaivePassword = "legacy-pass"
	nonString.ProtocolFields = map[string]any{"naiveUsername": 123, "naivePassword": 456}
	if err := req.Validate(nonString); err != nil {
		t.Fatalf("non-string ProtocolFields must fall back to settings: %v", err)
	}
	// ...and with no legacy value behind them they must fail.
	nonStringOnly := base
	nonStringOnly.ProtocolFields = map[string]any{"naiveUsername": 123, "naivePassword": 456}
	if err := req.Validate(nonStringOnly); err == nil {
		t.Fatal("non-string ProtocolFields with empty legacy fields must fail validation")
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
