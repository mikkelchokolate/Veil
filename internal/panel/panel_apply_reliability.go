package panel

// panelApplyReliabilityJS keeps mutating apply controls disabled when the plan
// request itself fails. A missing response must never be interpreted as an empty
// valid plan. It also serializes workflow commands so double-clicks cannot submit
// overlapping apply mutations or history requests that overwrite the same output.
func panelApplyReliabilityJS() string {
	return `    let applyWorkflowInFlight = false;
    let applyMutationButtonsDisabled = true;
    let applyConfigurationGeneration = 0;
    let applyPlanCurrent = false;

    setApplyMutationButtonsDisabled = function(disabled) {
      applyMutationButtonsDisabled = Boolean(disabled);
      ['apply-staged-files', 'apply-live-configs', 'reload-services'].forEach((id) => {
        const button = document.getElementById(id);
        if (button) button.disabled = applyMutationButtonsDisabled || applyWorkflowInFlight || isViewerRole();
      });
    };

    function setApplyPlanButtonDisabled(disabled) {
      const button = document.getElementById('build-apply-plan');
      if (button) button.disabled = Boolean(disabled);
    }

    function setApplyHistoryButtonDisabled(disabled) {
      const button = document.getElementById('load-apply-history');
      if (button) button.disabled = Boolean(disabled);
    }

    function setApplyWorkflowBusy(busy) {
      applyWorkflowInFlight = Boolean(busy);
      setApplyPlanButtonDisabled(applyWorkflowInFlight);
      setApplyHistoryButtonDisabled(applyWorkflowInFlight);
      setApplyMutationButtonsDisabled(applyMutationButtonsDisabled);
    }

    function renderApplyPlanUnavailable(message) {
      const warnings = document.getElementById('apply-safety-warnings');
      if (warnings) warnings.textContent = message || veilT('apply.warningInvalid');
      const body = document.getElementById('apply-file-diff-preview-body');
      if (body) {
        body.textContent = '';
        const row = document.createElement('tr');
        const cell = document.createElement('td');
        cell.colSpan = 5;
        cell.textContent = veilT('apply.noOperations');
        row.appendChild(cell);
        body.appendChild(row);
      }
      renderApplyRuntimes(null);
    }

    function invalidateApplyPlan(message) {
      applyPlanCurrent = false;
      setApplyMutationButtonsDisabled(true);
      renderApplyPlanUnavailable(message);
    }

    document.addEventListener('veil:configuration-changed', () => {
      applyConfigurationGeneration += 1;
      invalidateApplyPlan(veilT('apply.warningInvalid'));
    });

    runApplyWorkflowCommand = async function(command) {
      if (applyWorkflowInFlight) return null;
      const configurationGeneration = applyConfigurationGeneration;
      setApplyWorkflowBusy(true);
      try {
        const options = { method: 'POST' };
        if (command.request && Object.keys(command.request).length > 0) {
          options.headers = { 'Content-Type': 'application/json' };
          options.body = JSON.stringify(command.request);
        }
        const result = await loadJSON(command.path, 'apply-plan-output', options);
        if (configurationGeneration !== applyConfigurationGeneration) {
          invalidateApplyPlan(veilT('apply.warningInvalid'));
          return result;
        }
        if (result === null) {
          invalidateApplyPlan(veilT('apply.warningInvalid'));
          return null;
        }
        renderApplyRuntimes(result);
        renderApplySafePreview(result);
        const plan = applyPlanFromResponse(result);
        applyPlanCurrent = Boolean(plan && plan.valid === true);
        if (!applyPlanCurrent) setApplyMutationButtonsDisabled(true);
        return result;
      } finally {
        setApplyWorkflowBusy(false);
        applyViewerRoleGuard();
      }
    };

    loadApplyHistory = async function() {
      if (applyWorkflowInFlight) return null;
      const limitInput = document.getElementById('apply-history-limit');
      if (limitInput && !limitInput.checkValidity()) {
        limitInput.reportValidity();
        return null;
      }
      const rawLimit = limitInput ? limitInput.value.trim() : '';
      if (rawLimit && !Number.isInteger(Number(rawLimit))) {
        limitInput.setCustomValidity('Enter a whole number.');
        limitInput.reportValidity();
        limitInput.setCustomValidity('');
        return null;
      }
      invalidateApplyPlan(veilT('apply.warningInvalid'));
      setApplyWorkflowBusy(true);
      try {
        return await loadJSON(applyHistoryPath(), 'apply-plan-output');
      } finally {
        setApplyWorkflowBusy(false);
        applyViewerRoleGuard();
      }
    };
`
}
