package api

import (
	"strings"
	"testing"
)

func TestPanelServiceStatusCardModuleRendersOperationalControls(t *testing.T) {
	card := panelServiceStatusCardHTML()
	for _, want := range []string{
		`<h2>Service status</h2>`,
		`id="load-service-status"`,
		`id="toggle-auto-refresh"`,
		`id="service-status-output"`,
		`id="restart-veil"`,
		`id="restart-caddy"`,
		`id="restart-hysteria2"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Service status card missing %q", want)
		}
	}
}

func TestPanelRoutingCardModuleRendersRulesAndPresetControls(t *testing.T) {
	card := panelRoutingCardHTML()
	for _, want := range []string{
		`<h2>Routing rules</h2>`,
		`id="routing-rule-form"`,
		`id="routing-rule-name"`,
		`id="routing-rule-outbound"`,
		`id="routing-preset-profile"`,
		`id="apply-routing-preset"`,
		`id="routing-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Routing card missing %q", want)
		}
	}
}

func TestPanelWarpCardModuleRendersRedactedWarpControls(t *testing.T) {
	card := panelWarpCardHTML()
	for _, want := range []string{
		`<h2>WARP</h2>`,
		`id="warp-form"`,
		`id="warp-private-key"`,
		`id="warp-license-key"`,
		`[REDACTED]`,
		`id="save-warp-config"`,
		`id="warp-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("WARP card missing %q", want)
		}
	}
}

func TestPanelSettingsCardModuleRendersRedactedSettingsControls(t *testing.T) {
	card := panelSettingsCardHTML()
	for _, want := range []string{
		`<h2>Settings</h2>`,
		`id="settings-form"`,
		`id="settings-panel-listen"`,
		`id="settings-naive-password"`,
		`id="settings-hysteria2-password"`,
		`[REDACTED]`,
		`id="save-settings"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Settings card missing %q", want)
		}
	}
}

func TestPanelClientLinksCardModuleRendersCredentialDisclosureControls(t *testing.T) {
	card := panelClientLinksCardHTML()
	for _, want := range []string{
		`<h2>Client links</h2>`,
		`id="load-client-links"`,
		`id="load-client-subscription"`,
		`id="load-client-subscription-raw"`,
		`id="download-client-subscription"`,
		`id="copy-client-links"`,
		`id="client-links-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Client links card missing %q", want)
		}
	}
}

func TestPanelApplyCardModuleRendersApplyControls(t *testing.T) {
	card := panelApplyCardHTML()
	for _, want := range []string{
		`<h2>Apply plan</h2>`,
		`id="build-apply-plan"`,
		`id="apply-staged-files"`,
		`id="apply-live-configs"`,
		`id="reload-services"`,
		`id="load-apply-history"`,
		`id="apply-plan-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Apply card missing %q", want)
		}
	}
}

func TestPanelInboundFormModuleRendersInboundAndClientProfileControls(t *testing.T) {
	form := panelInboundFormHTML()
	for _, want := range []string{
		`<h2>Inbounds</h2>`,
		`id="inbound-form"`,
		`id="inbound-password"`,
		panelClientProfileControlsPlaceholder,
		`id="save-inbound"`,
		`id="delete-inbound"`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("Inbound form missing %q", want)
		}
	}
}

func TestPanelClientProfileFormModuleRendersControlsAndActions(t *testing.T) {
	controls := panelClientProfileControlsHTML()
	for _, want := range []string{
		`id="client-profile-name"`,
		`id="client-profile-username"`,
		`id="client-profile-password"`,
		`onclick="genClientProfilePassword()"`,
		`onclick="addClientProfile()"`,
	} {
		if !strings.Contains(controls, want) {
			t.Fatalf("Client profile controls missing %q", want)
		}
	}

	actions := panelClientProfileActionsJS()
	for _, want := range []string{
		`function genClientProfilePassword()`,
		`function addClientProfile()`,
		`Client profile name is required`,
		`profiles.push({ name, username: username || name, password, enabled: true })`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client profile actions missing %q", want)
		}
	}
}

func TestPanelHTMLIncludesInboundPasswordGenerationUI(t *testing.T) {
	html := panelHTML("/secret/")
	for _, want := range []string{
		`id="inbound-password"`,
		`id="inbound-profiles"`,
		`id="client-profile-name"`,
		`id="client-profile-username"`,
		`id="client-profile-password"`,
		`addClientProfile()`,
		`genClientProfilePassword()`,
		`Client profiles`,
		`genInboundPassword()`,
		`Generate`,
		`auto-generated if empty`,
		`payload.password`,
		`payload.profiles`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel HTML missing %q", want)
		}
	}
}
