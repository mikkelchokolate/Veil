package service

import veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"

type ManualActionResponse struct {
	Service string `json:"service"`
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ManualServiceControl struct {
	catalog ManagedRuntimeCatalog
	runner  RuntimeCommandRunner
}

func NewManualServiceControl(catalog ManagedRuntimeCatalog, runner RuntimeCommandRunner) ManualServiceControl {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	return ManualServiceControl{catalog: catalog, runner: runner}
}

func (c ManualServiceControl) Allows(name string) bool {
	return c.catalog.AllowsActionName(name)
}

func (c ManualServiceControl) Build(name, action string) ([]string, bool) {
	return c.catalog.ServiceActionCommand(name, action)
}

func (c ManualServiceControl) Run(name, action string) ManualActionResponse {
	resp := ManualActionResponse{Service: name, Action: action}
	command, ok := c.Build(name, action)
	if !ok {
		resp.Error = "unknown service: " + name
		return resp
	}
	out := c.runner.Run(veilruntime.RuntimeCommandInput{Command: command})
	resp.Output = out.Output
	if out.NotFound {
		resp.Error = command[0] + " not found"
		return resp
	}
	if out.TimedOut {
		resp.Error = "service action timed out"
		return resp
	}
	if out.Err != nil {
		resp.Error = out.Err.Error()
		return resp
	}
	resp.Success = true
	return resp
}
