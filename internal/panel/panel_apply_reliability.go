package panel

// panelApplyReliabilityJS keeps mutating apply controls disabled when the plan
// request itself fails. A missing response must never be interpreted as an empty
// valid plan. It also serializes workflow commands so double-clicks cannot submit
// overlapping apply mutations.
func panelApplyReliabilityJS() string {
	return `    let applyWorkflowInFlight = false;
    let applyMutationButtonsDisabled = true;

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

    runApplyWorkflowCommand = async function(command) {
      if (applyWorkflowInFlight) return null;
      applyWorkflowInFlight = true;
      setApplyPlanButtonDisabled(true);
      setApplyMutationButtonsDisabled(applyMutationButtonsDisabled);
      try {
        const options = { method: 'POST' };
        if (command.request && Object.keys(command.request).length > 0) {
          options.headers = { 'Content-Type': 'application/json' };
          options.body = JSON.stringify(command.request);
        }
        const result = await loadJSON(command.path, 'apply-plan-output', options);
        if (result === null) {
          setApplyMutationButtonsDisabled(true);
          const warnings = document.getElementById('apply-safety-warnings');
          if (warnings) warnings.textContent = veilT('apply.warningInvalid');
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
          return null;
        }
        renderApplyRuntimes(result);
        renderApplySafePreview(result);
        return result;
      } finally {
        applyWorkflowInFlight = false;
        setApplyPlanButtonDisabled(false);
        setApplyMutationButtonsDisabled(applyMutationButtonsDisabled);
        applyViewerRoleGuard();
      }
    };
`
}
