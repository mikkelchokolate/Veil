package panel

import (
	"strings"
	"testing"
)

func TestTranslationCatalogsCoverCorePanelSurfaces(t *testing.T) {
	catalogs := TranslationCatalogs()
	for _, locale := range []string{LocaleEnglish, LocaleRussian} {
		catalog, ok := catalogs[locale]
		if !ok {
			t.Fatalf("missing %s catalog", locale)
		}
		for _, key := range []string{
			"language.label",
			"a11y.skip",
			"a11y.primaryNavigation",
			"nav.dashboard",
			"nav.inbounds",
			"nav.routing",
			"nav.diagnostics",
			"nav.backups",
			"nav.users",
			"auth.username",
			"auth.password",
			"auth.login",
			"setup.title",
			"users.title",
			"users.managementTitle",
			"backups.title",
			"backups.disasterRecovery",
			"apply.title",
			"apply.safePreview",
			"diagnostics.logs",
			"inbounds.title",
			"routing.title",
			"clientLinks.title",
			"modal.addInbound",
			"placeholder.domain",
			"validation.ready",
			"dialog.close",
			"action.save",
			"action.cancel",
			"status.loading",
			"status.ready",
			"status.notLoaded",
			"confirm.deleteInbound",
			"confirm.deleteRoutingRule",
			"confirm.pruneBackups",
		} {
			if strings.TrimSpace(catalog[key]) == "" {
				t.Errorf("%s catalog missing %q", locale, key)
			}
		}
	}
	if catalogs[LocaleEnglish]["nav.dashboard"] == catalogs[LocaleRussian]["nav.dashboard"] {
		t.Fatal("Russian catalog did not translate dashboard")
	}
}

func TestTranslationCatalogsHaveMatchingKeys(t *testing.T) {
	catalogs := TranslationCatalogs()
	english := catalogs[LocaleEnglish]
	russian := catalogs[LocaleRussian]
	for key := range english {
		if strings.TrimSpace(russian[key]) == "" {
			t.Errorf("Russian catalog missing %q", key)
		}
	}
	for key := range russian {
		if strings.TrimSpace(english[key]) == "" {
			t.Errorf("English catalog missing %q", key)
		}
	}
}

func TestRussianCatalogCoversOperatorDashboard(t *testing.T) {
	catalog := TranslationCatalogs()[LocaleRussian]
	for _, key := range []string{
		"dashboard.managementSummary",
		"dashboard.systemOnline",
		"dashboard.coreUptime",
		"dashboard.monitoringSummary",
		"dashboard.serviceLinks",
		"token.title",
		"token.summaryStart",
		"version.title",
		"version.load",
		"installPreview.title",
		"installPreview.submit",
		"runtime.rawJSON",
		"runtime.interface",
		"runtime.bytesReceived",
		"runtime.bytesTransmitted",
		"runtime.packets",
		"runtime.noInterfaces",
		"runtime.noProcesses",
		"service.restart",
		"inbounds.empty",
		"clientLinks.load",
		"clientLinks.qrPrivacy",
		"routing.clearConsole",
		"warp.enabledHelp",
		"diagnostics.notStarted",
	} {
		value := strings.TrimSpace(catalog[key])
		if value == "" {
			t.Errorf("Russian catalog missing %q", key)
		}
		if value == translationCatalogs[LocaleEnglish][key] {
			t.Errorf("Russian catalog did not translate %q", key)
		}
	}
}

func TestDynamicPanelMessagesUseTranslationKeys(t *testing.T) {
	checks := map[string]string{
		"intro":          panelIntroActionsJS(),
		"users":          panelUsersActionsJS(),
		"client links":   panelClientLinksActionsJS(),
		"backups":        panelBackupsActionsJS(),
		"inbounds":       panelInboundActionsJS(),
		"client profile": panelClientProfileActionsJS(),
		"diagnostics":    DiagnosticsActionsJS(),
		"service status": ServiceStatusActionsJS(),
		"apply":          NewApplyWorkflowCommandCatalog().PanelActionsJS(),
	}
	for name, source := range checks {
		if !strings.Contains(source, "veilT(") {
			t.Errorf("%s actions do not use localization", name)
		}
	}
	for _, forbidden := range []string{
		"'Auto-refresh: OFF'",
		"'Auto-refresh: ON (10s)'",
		"'Verify'",
		"'Download'",
		"'Restore'",
		"'Configuration is ready to save.'",
		"'Validation runs as fields change.'",
		"'No managed files or runtime actions were reported.'",
		"'Console cleared.'",
	} {
		for name, source := range checks {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s actions retain untranslated literal %s", name, forbidden)
			}
		}
	}
}

func TestValidationIssueTranslationsCoverStableCodes(t *testing.T) {
	catalogs := TranslationCatalogs()
	for _, code := range []string{
		"dns_unresolved",
		"name_required",
		"unsupported_protocol",
		"transport_required",
		"unsupported_transport",
		"port_invalid",
		"duplicate_binding",
		"reserved_panel_port",
		"port_probe_failed",
		"port_in_use",
		"runtime_binary_missing",
		"runtime_unit_missing",
		"domain_required",
		"email_required",
		"credential_required",
	} {
		for _, locale := range SupportedLocales() {
			for _, suffix := range []string{"message", "remediation"} {
				key := "validation." + code + "." + suffix
				if strings.TrimSpace(catalogs[locale][key]) == "" {
					t.Errorf("%s catalog missing %q", locale, key)
				}
			}
		}
	}
	if !strings.Contains(LocalizationRuntimeJS(), "veilValidationIssueText") {
		t.Fatal("localization runtime does not expose validation issue translation")
	}
	for name, source := range map[string]string{
		"inbounds": panelInboundActionsJS(),
		"apply":    NewApplyWorkflowCommandCatalog().PanelActionsJS(),
	} {
		if !strings.Contains(source, "veilValidationIssueText") {
			t.Errorf("%s does not render localized validation issues", name)
		}
	}
}

func TestLocalizationRuntimeTranslatesDynamicContentAndPersistsSelection(t *testing.T) {
	runtime := LocalizationRuntimeJS()
	for _, want := range []string{
		"window.veilT",
		"MutationObserver",
		"veil_locale",
		"/api/auth/locale",
		"X-CSRF-Token",
		"localStorage.getItem('veil_csrf_token')",
		"data-veil-locale-select",
		"document.documentElement.lang",
	} {
		if !strings.Contains(runtime, want) {
			t.Fatalf("localization runtime missing %q", want)
		}
	}
}

func TestVariablePanelPhrasesUseTranslationKeys(t *testing.T) {
	checks := map[string]string{
		"inbound edit title":    panelInboundActionsJS(),
		"routing confirmation":  panelRoutingActionsJS(),
		"client links title":    panelClientLinksActionsJS(),
		"backup confirmation":   panelBackupsActionsJS(),
		"apply warning summary": NewApplyWorkflowCommandCatalog().PanelActionsJS(),
	}
	wants := map[string]string{
		"inbound edit title":    `veilT('inbounds.editTitle'`,
		"routing confirmation":  `veilT('confirm.deleteRoutingRule'`,
		"client links title":    `veilT('clientLinks.inboundTitle'`,
		"backup confirmation":   `veilT('confirm.pruneBackups'`,
		"apply warning summary": `veilT('apply.warningSummary'`,
	}
	for name, source := range checks {
		if !strings.Contains(source, wants[name]) {
			t.Errorf("%s missing %q", name, wants[name])
		}
	}
}
