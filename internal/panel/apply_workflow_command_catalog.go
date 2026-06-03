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
      output.textContent = runtimes.length === 0 ? 'Runtime units: none required' : 'Runtime units: ' + runtimes.join(', ');
    }

    async function runApplyWorkflowCommand(command) {
      const options = { method: 'POST' };
      if (command.request && Object.keys(command.request).length > 0) {
        options.headers = { 'Content-Type': 'application/json' };
        options.body = JSON.stringify(command.request);
      }
      const result = await loadJSON(command.path, 'apply-plan-output', options);
      renderApplyRuntimes(result);
    }

`)
	b.WriteString("    applyWorkflowCommands.forEach((command) => {\n")
	b.WriteString("      document.getElementById(command.buttonId).addEventListener('click', () => runApplyWorkflowCommand(command));\n")
	b.WriteString("    });")
	return b.String()
}
