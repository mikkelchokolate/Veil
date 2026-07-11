package panel

import (
	"strings"
	"testing"
)

func TestPanelApplyPlanInvalidatesAfterConfigurationMutation(t *testing.T) {
	actions := panelApplyActionsJS()
	for _, want := range []string{
		`let applyConfigurationGeneration = 0;`,
		`document.addEventListener('veil:configuration-changed'`,
		`applyConfigurationGeneration += 1;`,
		`function invalidateApplyPlan(message)`,
		`setApplyMutationButtonsDisabled(true);`,
		`const configurationGeneration = applyConfigurationGeneration;`,
		`configurationGeneration !== applyConfigurationGeneration`,
		`invalidateApplyPlan(veilT('apply.warningInvalid'));`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Apply stale-plan protection missing %q", want)
		}
	}
}

func TestPanelConfigurationMutationsPublishApplyInvalidationEvent(t *testing.T) {
	requests := panelRequestReliabilityJS()
	for _, want := range []string{
		`function isPanelConfigurationMutation(path, options)`,
		`endpoint === '/api/settings'`,
		`endpoint === '/api/warp'`,
		`endpoint.startsWith('/api/inbounds/')`,
		`endpoint.startsWith('/api/routing/rules/')`,
		`endpoint.startsWith('/api/routing/presets/')`,
		`new CustomEvent('veil:configuration-changed'`,
		`if (configurationChanged) notifyPanelConfigurationChanged(path);`,
	} {
		if !strings.Contains(requests, want) {
			t.Fatalf("Configuration mutation notification missing %q", want)
		}
	}

	settings := panelSettingsReliabilityJS()
	if !strings.Contains(settings, `notifyPanelConfigurationChanged('/api/settings');`) {
		t.Fatal("Settings save does not invalidate the current apply plan")
	}
}

func TestPanelApplyHistoryRevokesCurrentPlan(t *testing.T) {
	actions := panelApplyActionsJS()
	marker := `invalidateApplyPlan(veilT('apply.warningInvalid'));
      setApplyWorkflowBusy(true);`
	if !strings.Contains(actions, marker) {
		t.Fatal("Loading apply history does not revoke the current apply plan")
	}
}
