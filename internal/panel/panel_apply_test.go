package panel

import (
	"strings"
	"testing"
)

func TestPanelApplyActionsModuleRendersApplyWorkflowActions(t *testing.T) {
	actions := panelApplyActionsJS()
	for _, want := range []string{
		`function applyHistoryPath()`,
		`loadApplyHistory = async function()`,
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
		`setApplyMutationButtonsDisabled = function(disabled)`,
		`if (result === null)`,
		`setApplyMutationButtonsDisabled(true)`,
		`setApplyMutationButtonsDisabled(!plan || plan.valid !== true)`,
		`return null`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Apply actions missing %q", want)
		}
	}
}

func TestPanelApplyWorkflowSerializesCommandsAndRestoresPlanState(t *testing.T) {
	actions := panelApplyActionsJS()
	for _, want := range []string{
		`let applyWorkflowInFlight = false;`,
		`let applyMutationButtonsDisabled = true;`,
		`if (applyWorkflowInFlight) return null;`,
		`function setApplyWorkflowBusy(busy)`,
		`applyWorkflowInFlight = Boolean(busy);`,
		`button.disabled = applyMutationButtonsDisabled || applyWorkflowInFlight || isViewerRole();`,
		`setApplyWorkflowBusy(true);`,
		`finally {`,
		`setApplyWorkflowBusy(false);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Apply workflow single-flight guard missing %q", want)
		}
	}
}

func TestPanelApplyHistoryUsesWorkflowLockAndValidatesLimit(t *testing.T) {
	actions := panelApplyActionsJS()
	for _, want := range []string{
		`loadApplyHistory = async function()`,
		`if (applyWorkflowInFlight) return null;`,
		`limitInput && !limitInput.checkValidity()`,
		`!Number.isInteger(Number(rawLimit))`,
		`limitInput.reportValidity();`,
		`return await loadJSON(applyHistoryPath(), 'apply-plan-output');`,
		`setApplyHistoryButtonDisabled(applyWorkflowInFlight);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Apply history reliability missing %q", want)
		}
	}
}

func TestPanelApplyCardModuleRendersApplyControls(t *testing.T) {
	card := panelApplyCardHTML()
	for _, want := range []string{
		`<h2>Apply plan</h2>`,
		`id="build-apply-plan"`,
		`id="apply-staged-files" type="button" disabled`,
		`id="apply-live-configs" type="button" disabled`,
		`id="reload-services" type="button" disabled`,
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
