package api

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

    document.getElementById('build-apply-plan').addEventListener('click', async () => {
      await loadJSON('/api/apply/plan', 'apply-plan-output', { method: 'POST' });
    });

    document.getElementById('apply-staged-files').addEventListener('click', async () => {
      await loadJSON('/api/apply', 'apply-plan-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      });
    });

    document.getElementById('apply-live-configs').addEventListener('click', async () => {
      await loadJSON('/api/apply', 'apply-plan-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true, applyLive: true })
      });
    });

    document.getElementById('reload-services').addEventListener('click', async () => {
      await loadJSON('/api/apply', 'apply-plan-output', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true, applyLive: true, applyServices: true })
      });
    });

    document.getElementById('load-apply-history').addEventListener('click', loadApplyHistory);`
}
