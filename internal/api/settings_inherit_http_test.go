package api

import (
	"net/http"
	"testing"
)

// TestSettingsPartialPutPreservesOmittedNonSchemaFields is the regression for
// the legacy server-rendered panel, which submits only
// panelListen/panelAccess/webBasePath/mode/domain/email/protocolFields. The
// PUT must inherit the omitted non-schema fields (firewallManagement, port
// defaults, ACME fields) from the live state instead of zeroing them.
func TestSettingsPartialPutPreservesOmittedNonSchemaFields(t *testing.T) {
	r, state := newSettingsEchoRouter(t)

	// Seed a full settings object including the non-schema fields and the
	// schema keys mirrored into protocolFields (as the SPA echo does).
	seed := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com",
		"firewallManagement":false,"defaultInboundPublicPort":443,
		"defaultAcmeEmail":"acme@example.com","acmeChallengeMode":"dns-01",
		"protocolFields":{
			"naiveUsername":"veil","panelDomain":"panel.example.com",
			"panelEmail":"panel@example.com","panelPublicPort":8443}}`
	if code := putSettings(t, r, seed); code != http.StatusOK {
		t.Fatalf("seed put: %d", code)
	}

	// Legacy-style partial payload (omits all non-schema fields).
	partial := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com",
		"protocolFields":{"naiveUsername":"veil"}}`
	if code := putSettings(t, r, partial); code != http.StatusOK {
		t.Fatalf("partial put: %d", code)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.settings.FirewallManagement == nil || *state.settings.FirewallManagement {
		t.Fatalf("firewallManagement was not inherited (nil or true): %+v", state.settings.FirewallManagement)
	}
	if state.settings.DefaultInboundPublicPort != 443 {
		t.Fatalf("defaultInboundPublicPort = %d, want 443", state.settings.DefaultInboundPublicPort)
	}
	if state.settings.DefaultAcmeEmail != "acme@example.com" {
		t.Fatalf("defaultAcmeEmail = %q", state.settings.DefaultAcmeEmail)
	}
	if state.settings.AcmeChallengeMode != "dns-01" {
		t.Fatalf("acmeChallengeMode = %q", state.settings.AcmeChallengeMode)
	}
}

// TestSettingsPartialPutDoesNotClobberSchemaFieldsFromProtocolFields ensures
// that schema-declared keys submitted only via protocolFields (legacy panel
// style) survive the PUT and reach the flat field they back, instead of being
// overridden by the inherited stale flat value.
func TestSettingsPartialPutDoesNotClobberSchemaFieldsFromProtocolFields(t *testing.T) {
	r, state := newSettingsEchoRouter(t)

	seed := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com",
		"protocolFields":{"naiveUsername":"veil","panelPublicPort":443}}`
	if code := putSettings(t, r, seed); code != http.StatusOK {
		t.Fatalf("seed put: %d", code)
	}

	// Legacy panel edits panelPublicPort via protocolFields only.
	edit := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com",
		"protocolFields":{"naiveUsername":"veil","panelPublicPort":9443}}`
	if code := putSettings(t, r, edit); code != http.StatusOK {
		t.Fatalf("edit put: %d", code)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.settings.PanelPublicPort != 9443 {
		t.Fatalf("panelPublicPort = %d, want 9443 (pf edit clobbered by inherited flat)", state.settings.PanelPublicPort)
	}
}

// TestSettingsExplicitZeroStillHonored ensures that when the SPA explicitly
// sends a zero value for a non-schema field (both the flat and the
// protocolFields copy, as the SPA echo does), the explicit value wins over
// the inherited one.
func TestSettingsExplicitZeroStillHonored(t *testing.T) {
	r, state := newSettingsEchoRouter(t)

	seed := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","protocolFields":{"panelPublicPort":8443}}`
	if code := putSettings(t, r, seed); code != http.StatusOK {
		t.Fatalf("seed put: %d", code)
	}

	// SPA-style clear: both the flat and the protocolFields copy go to 0.
	clear := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","panelPublicPort":0,"protocolFields":{"panelPublicPort":0}}`
	if code := putSettings(t, r, clear); code != http.StatusOK {
		t.Fatalf("clear put: %d", code)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.settings.PanelPublicPort != 0 {
		t.Fatalf("explicit panelPublicPort=0 was overridden: %d", state.settings.PanelPublicPort)
	}
}
