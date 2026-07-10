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
		`veilT('apply.runtimeNone')`,
		`function renderApplySafePreview(data)`,
		`apply-file-diff-preview-body`,
		`apply-safety-warnings`,
		`No file content is shown`,
		`function setApplyMutationButtonsDisabled(disabled)`,
		`if (result === null)`,
		`setApplyMutationButtonsDisabled(true)`,
		`return null`,
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
		`id="apply-safety-warnings"`,
		`id="apply-file-diff-preview-body"`,
		`Safe apply preview`,
		`Runtime units: not planned`,
		`id="apply-plan-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Apply card missing %q", want)
		}
	}
}

func TestPanelApplyPreviewUsesStructuredOperationsWithCompatibilityFallback(t *testing.T) {
	actions := panelApplyActionsJS()
	for _, want := range []string{
		`Array.isArray(plan.operations)`,
		`operation.interruptionRisk`,
		`operation.rollbackAvailable`,
		`operation.validationSource`,
		`Structured operation`,
		`appendApplyPreviewRows`,
		`veilT('apply.warningInvalid')`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Structured apply preview missing %q", want)
		}
	}
	card := panelApplyCardHTML()
	for _, want := range []string{
		`<th>Operation</th>`,
		`<th>Target</th>`,
		`<th>Interruption risk</th>`,
		`<th>Rollback</th>`,
		`<th>Validated by</th>`,
		`aria-live="polite"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Structured apply card missing %q", want)
		}
	}
}
