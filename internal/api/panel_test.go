package api

import (
	"strings"
	"testing"
)

func TestPanelIntroActionsModuleRendersTokenPreviewAndVersionActions(t *testing.T) {
	actions := panelIntroActionsJS()
	for _, want := range []string{
		`veil_api_token`,
		`function authHeaders()`,
		`async function loadJSON(path, outputId, options)`,
		`profile-preview-form`,
		`/api/profiles/ru-recommended/preview`,
		`load-version`,
		`/api/version`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Intro actions missing %q", want)
		}
	}
}

func TestPanelIntroCardsModuleRendersOverviewVersionTokenAndPreview(t *testing.T) {
	cards := panelIntroCardsHTML()
	for _, want := range []string{
		`Veil Panel`,
		`<h2>Version</h2>`,
		`id="load-version"`,
		`<h2>API token</h2>`,
		`id="api-token"`,
		`<h2>Profile preview</h2>`,
		`id="profile-preview-form"`,
		`id="profile-preview-output"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("Intro cards missing %q", want)
		}
	}
}

func TestPanelDiagnosticsActionsModuleRendersToolActions(t *testing.T) {
	actions := panelDiagnosticsActionsJS()
	for _, want := range []string{
		`run-speedtest`,
		`/api/tools/speedtest`,
		`load-logs`,
		`/api/logs?unit=`,
		`load-firewall`,
		`/api/firewall`,
		`run-dns-lookup`,
		`/api/tools/dns-lookup`,
		`run-ping`,
		`/api/tools/ping`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Diagnostics actions missing %q", want)
		}
	}
}

func TestPanelDiagnosticsCardsModuleRendersToolControls(t *testing.T) {
	cards := panelDiagnosticsCardsHTML()
	for _, want := range []string{
		`<h2>Speedtest</h2>`,
		`id="run-speedtest"`,
		`<h2>DNS lookup</h2>`,
		`id="run-dns-lookup"`,
		`<h2>Ping</h2>`,
		`id="run-ping"`,
		`<h2>Firewall</h2>`,
		`id="load-firewall"`,
		`<h2>Service logs</h2>`,
		`id="load-logs"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("Diagnostics cards missing %q", want)
		}
	}
}

func TestPanelRuntimeStatsActionsModuleRendersRuntimeLoadActions(t *testing.T) {
	actions := panelRuntimeStatsActionsJS()
	for _, want := range []string{
		`load-system-stats`,
		`/api/system`,
		`load-network-stats`,
		`/api/network`,
		`load-connections-stats`,
		`/api/connections`,
		`load-processes-stats`,
		`/api/processes`,
		`load-disk-stats`,
		`/api/disk`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Runtime stats actions missing %q", want)
		}
	}
}

func TestPanelRuntimeStatsCardsModuleRendersRuntimeControls(t *testing.T) {
	cards := panelRuntimeStatsCardsHTML()
	for _, want := range []string{
		`<h2>System resources</h2>`,
		`id="load-system-stats"`,
		`<h2>Network interfaces</h2>`,
		`id="load-network-stats"`,
		`<h2>Listening ports</h2>`,
		`id="load-connections-stats"`,
		`<h2>Managed processes</h2>`,
		`id="load-processes-stats"`,
		`<h2>Disk usage</h2>`,
		`id="load-disk-stats"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("Runtime stats cards missing %q", want)
		}
	}
}

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

func TestPanelRoutingActionsModuleRendersRuleAndPresetActions(t *testing.T) {
	actions := panelRoutingActionsJS()
	for _, want := range []string{
		`async function saveRoutingRule(event)`,
		`async function deleteRoutingRule()`,
		`async function applyRoutingPreset()`,
		`/api/routing/rules`,
		`/api/routing/presets/`,
		`routing-rule-name`,
		`routing-preset-profile`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Routing actions missing %q", want)
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

func TestPanelWarpActionsModuleRendersLoadAndSaveActions(t *testing.T) {
	actions := panelWarpActionsJS()
	for _, want := range []string{
		`async function loadWarpIntoForm()`,
		`async function saveWarpConfig(event)`,
		`/api/warp`,
		`warp-private-key`,
		`parseReserved`,
		`socksPort`,
		`mtu`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("WARP actions missing %q", want)
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

func TestPanelSettingsActionsModuleRendersLoadAndSaveActions(t *testing.T) {
	actions := panelSettingsActionsJS()
	for _, want := range []string{
		`async function loadSettingsIntoForm()`,
		`async function saveSettings(event)`,
		`/api/settings`,
		`settings-naive-password`,
		`hysteria2Password`,
		`fallbackRoot`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Settings actions missing %q", want)
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

func TestPanelServiceStatusActionsModuleRendersAutoRefreshActions(t *testing.T) {
	actions := panelServiceStatusActionsJS()
	for _, want := range []string{
		`function loadServiceStatus()`,
		`let autoRefreshInterval = null`,
		`toggle-auto-refresh`,
		`setInterval(loadServiceStatus, 10000)`,
		`beforeunload`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Service status actions missing %q", want)
		}
	}
}

func TestPanelClientLinksActionsModuleRendersCredentialDisclosureActions(t *testing.T) {
	actions := panelClientLinksActionsJS()
	for _, want := range []string{
		`async function loadClientLinks()`,
		`async function loadClientSubscription()`,
		`async function loadRawClientSubscription()`,
		`async function copyClientLinksOutput()`,
		`async function downloadClientSubscriptionPath(path, filename)`,
		`navigator.clipboard.writeText`,
		`URL.createObjectURL`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client links actions missing %q", want)
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

func TestPanelApplyActionsModuleRendersApplyWorkflowActions(t *testing.T) {
	actions := panelApplyActionsJS()
	for _, want := range []string{
		`function applyHistoryPath()`,
		`async function loadApplyHistory()`,
		`build-apply-plan`,
		`apply-staged-files`,
		`apply-live-configs`,
		`reload-services`,
		`load-apply-history`,
		`applyServices: true`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Apply actions missing %q", want)
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

func TestPanelInboundActionsModuleRendersInboundManagementActions(t *testing.T) {
	actions := panelInboundActionsJS()
	for _, want := range []string{
		`async function loadInboundsIntoOutput()`,
		`function randomPassword()`,
		`function genInboundPassword()`,
		`async function saveInbound(event)`,
		`async function deleteInbound()`,
		`payload.profiles`,
		`/api/inbounds`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Inbound actions missing %q", want)
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
