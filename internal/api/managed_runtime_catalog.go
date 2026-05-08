package api

import "path/filepath"

type ManagedRuntime struct {
	Name             string
	ActionName       string
	Protocol         string
	Transport        string
	Unit             string
	PromotedSubpath  string
	PromotedVerb     string
	ManualRestart    bool
	HealthCheckAfter bool
}

type ManagedRuntimeCatalog struct {
	runtimes []ManagedRuntime
}

func NewManagedRuntimeCatalog() ManagedRuntimeCatalog {
	return ManagedRuntimeCatalog{runtimes: []ManagedRuntime{
		{Name: "veil", ActionName: "veil", Unit: "veil.service", ManualRestart: true},
		{Name: "naive", ActionName: "caddy", Protocol: "naiveproxy", Transport: "tcp", Unit: "veil-naive.service", PromotedSubpath: "caddy/Caddyfile", PromotedVerb: "reload", ManualRestart: true, HealthCheckAfter: true},
		{Name: "hysteria2", ActionName: "hysteria2", Protocol: "hysteria2", Transport: "udp", Unit: "veil-hysteria2.service", PromotedSubpath: "hysteria2/server.yaml", PromotedVerb: "reload", ManualRestart: true, HealthCheckAfter: true},
		{Name: "sing-box", ActionName: "sing-box", Unit: "veil-warp.service", PromotedSubpath: "sing-box/warp.json", PromotedVerb: "reload", ManualRestart: true, HealthCheckAfter: true},
		{Name: "mieru", ActionName: "mieru", Protocol: "mieru", Unit: "veil-mieru.service", PromotedSubpath: "mieru/server_config.json", PromotedVerb: "restart", ManualRestart: true, HealthCheckAfter: true},
	}}
}

func (c ManagedRuntimeCatalog) Runtimes() []ManagedRuntime {
	runtimes := make([]ManagedRuntime, len(c.runtimes))
	copy(runtimes, c.runtimes)
	return runtimes
}

func (c ManagedRuntimeCatalog) ApplyAction(key string) (string, bool) {
	for _, runtime := range c.runtimes {
		if runtime.PromotedVerb == "" {
			continue
		}
		if runtime.Protocol == key || runtime.Name == key || runtime.ActionName == key {
			return runtime.PromotedVerb + " " + runtime.Unit, true
		}
	}
	return "", false
}

func (c ManagedRuntimeCatalog) ServiceActionCommand(actionName, action string) ([]string, bool) {
	for _, runtime := range c.runtimes {
		if runtime.ActionName == actionName && runtime.ManualRestart {
			return []string{"systemctl", action, runtime.Unit}, true
		}
	}
	return nil, false
}

func (c ManagedRuntimeCatalog) AllowsActionName(actionName string) bool {
	for _, runtime := range c.runtimes {
		if runtime.ActionName == actionName && runtime.ManualRestart {
			return true
		}
	}
	return false
}

func (c ManagedRuntimeCatalog) PromotedCommands(applyRoot string, liveFiles []string) [][]string {
	commands := [][]string{}
	for _, runtime := range c.runtimes {
		if runtime.PromotedSubpath == "" || runtime.PromotedVerb == "" {
			continue
		}
		if containsPath(liveFiles, filepath.Join(applyRoot, "live", filepath.FromSlash(runtime.PromotedSubpath))) {
			commands = append(commands, []string{"systemctl", runtime.PromotedVerb, runtime.Unit})
		}
	}
	return commands
}

func (c ManagedRuntimeCatalog) AllowsPromotedAction(command []string) bool {
	if len(command) != 3 || command[0] != "systemctl" {
		return false
	}
	for _, runtime := range c.runtimes {
		if runtime.PromotedVerb == "" {
			continue
		}
		if command[1] == runtime.PromotedVerb && command[2] == runtime.Unit {
			return true
		}
	}
	return false
}

func (c ManagedRuntimeCatalog) AllowsHealthUnit(unit string) bool {
	for _, runtime := range c.runtimes {
		if runtime.HealthCheckAfter && runtime.Unit == unit {
			return true
		}
	}
	return false
}
