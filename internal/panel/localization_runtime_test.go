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

func TestLocalizationRuntimeTranslatesDynamicContentAndPersistsSelection(t *testing.T) {
	runtime := LocalizationRuntimeJS()
	for _, want := range []string{
		"window.veilT",
		"MutationObserver",
		"veil_locale",
		"/api/auth/locale",
		"X-CSRF-Token",
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
