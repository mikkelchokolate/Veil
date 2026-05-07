package api

import (
	"strings"
	"testing"
)

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
