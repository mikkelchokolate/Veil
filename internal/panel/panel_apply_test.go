package panel

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
		`"applyServices":true`,
		`function renderApplyRuntimes(data)`,
		`data.plan && Array.isArray(data.plan.runtimes)`,
		`Runtime units: none required`,
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
		`id="apply-runtime-output"`,
		`Runtime units: not planned`,
		`id="apply-plan-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Apply card missing %q", want)
		}
	}
}
