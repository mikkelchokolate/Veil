package panel

import (
	"encoding/json"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type ApplyRequest = model.ApplyRequest

type ApplyWorkflowCommand struct {
	Name     string       `json:"name"`
	ButtonID string       `json:"buttonId"`
	Path     string       `json:"path"`
	Request  ApplyRequest `json:"request"`
}

type ApplyWorkflowCommandCatalog struct{}

func NewApplyWorkflowCommandCatalog() ApplyWorkflowCommandCatalog {
	return ApplyWorkflowCommandCatalog{}
}

func (ApplyWorkflowCommandCatalog) Commands() []ApplyWorkflowCommand {
	commands := []ApplyWorkflowCommand{
		{Name: "plan", ButtonID: "build-apply-plan", Path: "/api/apply/plan"},
		{Name: "stage", ButtonID: "apply-staged-files", Path: "/api/apply", Request: ApplyRequest{Confirm: true}},
		{Name: "promote-live", ButtonID: "apply-live-configs", Path: "/api/apply", Request: ApplyRequest{Confirm: true, ApplyLive: true}},
		{Name: "promote-live-services", ButtonID: "reload-services", Path: "/api/apply", Request: ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true}},
	}
	return append([]ApplyWorkflowCommand(nil), commands...)
}

func (c ApplyWorkflowCommandCatalog) PanelActionsJS() string {
	encoded, _ := json.Marshal(c.Commands())
	var b strings.Builder
	b.WriteString("    const applyWorkflowCommands = ")
	b.Write(encoded)
	b.WriteString(`;

    function applyRuntimesFromResponse(data) {
      if (!data) {
        return [];
      }
      if (Array.isArray(data.runtimes)) {
        return data.runtimes;
      }
      if (data.plan && Array.isArray(data.plan.runtimes)) {
        return data.plan.runtimes;
      }
      return [];
    }

    function renderApplyRuntimes(data) {
      const output = document.getElementById('apply-runtime-output');
      if (!output) {
        return;
      }
      const runtimes = applyRuntimesFromResponse(data);
      output.textContent = runtimes.length === 0
        ? veilT('apply.runtimeNone')
        : veilT('apply.runtimeList', { runtimes: runtimes.join(', ') });
    }

    function applyPlanFromResponse(data) {
      if (!data) {
        return {};
      }
      if (data.plan) {
        return data.plan;
      }
      return data;
    }

    function appendApplyPreviewRows(rows, stage, values, note) {
      if (!Array.isArray(values)) {
        return;
      }
      values.forEach((value) => {
        if (value === undefined || value === null || value === '') {
          return;
        }
        rows.push({ stage: stage, name: String(value), note: note });
      });
    }

    function applyFilesFromResponse(data) {
      const plan = applyPlanFromResponse(data);
      if (Array.isArray(plan.operations)) {
        return plan.operations.map((operation) => ({
          operation: String(operation.type || 'operation').replace(/_/g, ' '),
          target: operation.destination || operation.unit || operation.source || 'managed target',
          risk: operation.interruptionRisk || 'none',
          rollback: operation.rollbackAvailable ? 'available' : 'not available',
          validatedBy: operation.validationSource || 'not reported',
          note: 'Structured operation'
        }));
      }
      const rows = [];
      appendApplyPreviewRows(rows, 'generated', plan.configs, 'Generated managed config. No file content is shown.');
      appendApplyPreviewRows(rows, 'runtime plan', plan.actions, 'Planned service or runtime action.');
      appendApplyPreviewRows(rows, 'staged write', data && data.writtenFiles, 'Validated file written to the staged config set.');
      appendApplyPreviewRows(rows, 'live promote', data && data.liveFiles, 'Staged file promoted to the live runtime path.');
      appendApplyPreviewRows(rows, 'backup', data && data.backupFiles, 'Existing live file preserved before promotion.');
      appendApplyPreviewRows(rows, 'rollback', data && data.rollbackFiles, 'Rollback file available if reload or health checks fail.');
      return rows.map((entry) => ({
        operation: entry.stage,
        target: entry.name,
        risk: entry.stage === 'runtime plan' ? 'review action' : 'reload',
        rollback: entry.stage === 'backup' || entry.stage === 'rollback' ? 'available' : 'not reported',
        validatedBy: 'legacy plan fields',
        note: entry.note
      }));
    }

    function applyPreviewMetadata(data) {
      const chunks = [];
      const add = (value) => {
        if (!value) {
          return;
        }
        if (Array.isArray(value)) {
          value.forEach(add);
          return;
        }
        if (typeof value === 'object') {
          chunks.push(JSON.stringify(value));
          return;
        }
        chunks.push(String(value));
      };
      const plan = applyPlanFromResponse(data);
      add(plan.configs);
      add(plan.actions);
      add(plan.runtimes);
      if (data) {
        add(data.writtenFiles);
        add(data.liveFiles);
        add(data.validations);
        add(data.serviceActions);
        add(data.healthChecks);
      }
      return chunks.join(' ').toLowerCase();
    }

    function applyWarningsFromResponse(data) {
      const plan = applyPlanFromResponse(data);
      const warnings = [];
      const metadata = applyPreviewMetadata(data);
      if (plan.valid === false) {
        warnings.push(veilT('apply.warningInvalid'));
      }
      if (Array.isArray(plan.errors) && plan.errors.length > 0) {
        warnings.push(veilT('apply.warningErrors'));
      }
      if (Array.isArray(plan.issues)) {
        plan.issues.forEach((issue) => {
          if (issue && issue.severity !== 'info') {
            warnings.push(issue.message + (issue.remediation ? ' ' + issue.remediation : ''));
          }
        });
      }
      if (Array.isArray(plan.operations) && plan.operations.some((operation) => operation.interruptionRisk === 'connection-drop')) {
        warnings.push(veilT('apply.warningConnectionDrop'));
      }
      if (metadata.includes('firewall') || metadata.includes('ufw') || metadata.includes('iptables') || metadata.includes('nft')) {
        warnings.push(veilT('apply.warningFirewall'));
      }
      if (metadata.includes('dns') || metadata.includes('domain')) {
        warnings.push(veilT('apply.warningDNS'));
      }
      if (metadata.includes('tls') || metadata.includes('caddy') || metadata.includes('acme') || metadata.includes('cert') || metadata.includes(':443') || metadata.includes(' 443')) {
        warnings.push(veilT('apply.warningTLS'));
      }
      if (applyRuntimesFromResponse(data).length > 0 || metadata.includes('systemctl') || metadata.includes('reload') || metadata.includes('restart') || metadata.includes('service')) {
        warnings.push(veilT('apply.warningServiceRestart'));
      }
      if (warnings.length === 0) {
        warnings.push(veilT('apply.warningNone'));
      }
      return warnings;
    }

    function renderApplySafePreview(data) {
      const warningsOutput = document.getElementById('apply-safety-warnings');
      if (warningsOutput) {
        warningsOutput.textContent = veilT('apply.warningSummary', {
          warnings: applyWarningsFromResponse(data).join(' ')
        });
      }
      const body = document.getElementById('apply-file-diff-preview-body');
      if (!body) {
        return;
      }
      const rows = applyFilesFromResponse(data);
      body.textContent = '';
      const plan = applyPlanFromResponse(data);
      ['apply-staged-files', 'apply-live-configs', 'reload-services'].forEach((id) => {
        const button = document.getElementById(id);
        if (button) {
          button.disabled = plan.valid === false || isViewerRole();
        }
      });
      if (rows.length === 0) {
        const row = document.createElement('tr');
        const cell = document.createElement('td');
        cell.colSpan = 5;
        cell.textContent = 'No managed files or runtime actions were reported.';
        row.appendChild(cell);
        body.appendChild(row);
        return;
      }
      rows.forEach((entry) => {
        const row = document.createElement('tr');
        ['operation', 'target', 'risk', 'rollback', 'validatedBy'].forEach((field) => {
          const cell = document.createElement('td');
          cell.textContent = entry[field];
          row.appendChild(cell);
        });
        body.appendChild(row);
      });
    }

    async function runApplyWorkflowCommand(command) {
      const options = { method: 'POST' };
      if (command.request && Object.keys(command.request).length > 0) {
        options.headers = { 'Content-Type': 'application/json' };
        options.body = JSON.stringify(command.request);
      }
      const result = await loadJSON(command.path, 'apply-plan-output', options);
      renderApplyRuntimes(result);
      renderApplySafePreview(result);
    }

`)
	b.WriteString("    applyWorkflowCommands.forEach((command) => {\n")
	b.WriteString("      document.getElementById(command.buttonId).addEventListener('click', () => runApplyWorkflowCommand(command));\n")
	b.WriteString("    });")
	return b.String()
}
