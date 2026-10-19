package settings

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// panelIdentitySchemas mirrors the settings-scoped panel identity keys the
// naiveproxy protocol registers (internal/protocols/naiveproxy/ui.go).
func panelIdentitySchemas() []schema.FieldSchema {
	return append(testSettingsFieldSchemas(),
		schema.FieldSchema{Key: "panelDomain", Type: schema.FieldText, Scope: "settings"},
		schema.FieldSchema{Key: "panelEmail", Type: schema.FieldText, Scope: "settings"},
		schema.FieldSchema{Key: "panelPublicPort", Type: schema.FieldNumber, Default: 443, Scope: "settings"},
	)
}

func panelIdentityBase() Settings {
	return Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}
}

// Issue #1291: panelDomain/panelEmail/panelPublicPort were only type-coerced,
// so a wildcard host matcher, a garbage ACME contact or an unbindable port
// reached the Caddy renderer. Each must now be rejected whether it arrives as
// the flat field or through protocolFields (the panel UI path).
func TestSettingsValidationRejectsInvalidPanelIdentity(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Settings)
		wantErr string
	}{
		{"flat wildcard domain", func(s *Settings) { s.PanelDomain = "*" }, "panelDomain:"},
		{"flat wildcard subdomain", func(s *Settings) { s.PanelDomain = "*.attacker.example" }, "panelDomain:"},
		{"protocolFields wildcard domain", func(s *Settings) { s.ProtocolFields = map[string]any{"panelDomain": "*"} }, "panelDomain:"},
		{"url as domain", func(s *Settings) { s.PanelDomain = "https://panel.example.com" }, "panelDomain:"},
		{"dotless domain", func(s *Settings) { s.PanelDomain = "localhost" }, "panelDomain:"},
		{"bracketed ip domain", func(s *Settings) { s.PanelDomain = "[::1]" }, "panelDomain:"},
		{"flat garbage email", func(s *Settings) { s.PanelEmail = "not-an-email" }, "panelEmail:"},
		{"protocolFields display-name email", func(s *Settings) {
			s.ProtocolFields = map[string]any{"panelEmail": "Admin <admin@example.com>"}
		}, "panelEmail:"},
		{"flat port above range", func(s *Settings) { s.PanelPublicPort = 99999 }, "panelPublicPort must be"},
		{"flat negative port", func(s *Settings) { s.PanelPublicPort = -1 }, "panelPublicPort must be"},
		{"protocolFields port above range", func(s *Settings) {
			s.ProtocolFields = map[string]any{"panelPublicPort": float64(70000)}
		}, "panelPublicPort must be"},
		{"protocolFields negative port", func(s *Settings) {
			s.ProtocolFields = map[string]any{"panelPublicPort": float64(-443)}
		}, "panelPublicPort must be"},
		{"protocolFields fractional port", func(s *Settings) {
			s.ProtocolFields = map[string]any{"panelPublicPort": 443.5}
		}, "panelPublicPort must be"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settings := panelIdentityBase()
			tc.mutate(&settings)
			err := NewSettingsValidationWithFieldSchemas(panelIdentitySchemas()).NormalizeAndValidate(&settings, Settings{})
			if err == nil {
				t.Fatalf("NormalizeAndValidate accepted %+v", settings)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestSettingsValidationNormalizesValidPanelIdentity(t *testing.T) {
	settings := panelIdentityBase()
	settings.ProtocolFields = map[string]any{
		"panelDomain":     " Panel.Example.COM ",
		"panelEmail":      "admin@example.com",
		"panelPublicPort": float64(8443),
	}
	if err := NewSettingsValidationWithFieldSchemas(panelIdentitySchemas()).NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if settings.PanelDomain != "panel.example.com" {
		t.Fatalf("PanelDomain = %q, want folded panel.example.com", settings.PanelDomain)
	}
	// The stored protocolFields copy must agree with the flat field, or the
	// next GET echoes the unfolded value back into the form.
	if settings.ProtocolFields["panelDomain"] != "panel.example.com" {
		t.Fatalf("protocolFields.panelDomain = %#v, want folded copy", settings.ProtocolFields["panelDomain"])
	}
	if settings.PanelEmail != "admin@example.com" || settings.PanelPublicPort != 8443 {
		t.Fatalf("panel identity = %q/%d", settings.PanelEmail, settings.PanelPublicPort)
	}
}

func TestSettingsValidationAllowsUnsetPanelIdentity(t *testing.T) {
	settings := panelIdentityBase()
	if err := NewSettingsValidationWithFieldSchemas(panelIdentitySchemas()).NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if settings.PanelDomain != "" || settings.PanelEmail != "" {
		t.Fatalf("unset panel identity grew values: %q/%q", settings.PanelDomain, settings.PanelEmail)
	}
	if settings.PanelPublicPort != 443 {
		t.Fatalf("PanelPublicPort = %d, want schema default 443", settings.PanelPublicPort)
	}
}

// A legacy state that already persisted a wildcard must not keep riding
// along on unrelated PUTs: the inherited value fails closed, and the same
// PUT can repair it by supplying a valid domain.
func TestSettingsValidationInheritedWildcardPanelDomainFailsClosedUntilReplaced(t *testing.T) {
	current := panelIdentityBase()
	current.PanelDomain = "*"
	current.ProtocolFields = map[string]any{"panelDomain": "*"}

	update := panelIdentityBase()
	err := NewSettingsValidationWithFieldSchemas(panelIdentitySchemas()).NormalizeAndValidate(&update, current)
	if err == nil || !strings.Contains(err.Error(), "panelDomain:") {
		t.Fatalf("inherited wildcard err = %v, want panelDomain rejection", err)
	}

	repair := panelIdentityBase()
	repair.ProtocolFields = map[string]any{"panelDomain": "panel.example.com"}
	if err := NewSettingsValidationWithFieldSchemas(panelIdentitySchemas()).NormalizeAndValidate(&repair, current); err != nil {
		t.Fatalf("repairing PUT rejected: %v", err)
	}
	if repair.PanelDomain != "panel.example.com" {
		t.Fatalf("PanelDomain = %q after repair", repair.PanelDomain)
	}
}
