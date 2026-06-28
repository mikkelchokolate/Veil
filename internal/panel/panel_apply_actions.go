package panel

const panelApplyActionsPlaceholder = "__VEIL_PANEL_APPLY_ACTIONS__"

func panelApplyActionsJS() string {
	return `    function applyHistoryPath() {
      const params = new URLSearchParams();
      const stage = document.getElementById('apply-history-stage').value;
      const success = document.getElementById('apply-history-success').value;
      const limit = document.getElementById('apply-history-limit').value;
      if (stage) {
        params.set('stage', stage);
      }
      if (success) {
        params.set('success', success);
      }
      if (limit) {
        params.set('limit', limit);
      }
      const query = params.toString();
      return '/api/apply/history?' + query;
    }

    async function loadApplyHistory() {
      await loadJSON(applyHistoryPath(), 'apply-plan-output');
    }

    // applyWorkflowCommands is emitted by PanelActionsJS() as raw JavaScript.
    // The data is JSON-marshalled server-side and treated as code, not a quoted string.
    // lgtm[go/unsafe-quoting]
` + NewApplyWorkflowCommandCatalog().PanelActionsJS() + `

    document.getElementById('load-apply-history').addEventListener('click', loadApplyHistory);`
}
