package service

import (
	"path/filepath"
	"strings"
)

type ManagedRuntime struct {
	Name       string
	ActionName string
	Protocol   string
	Transport  string
	Unit       string
	// TemplateUnit is the un-instantiated template form (e.g. veil-caddy@.service).
	TemplateUnit     string
	PromotedSubpath  string
	PromotedVerb     string
	ManualRestart    bool
	HealthCheckAfter bool
}

type ManagedRuntimeCatalog struct {
	runtimes []ManagedRuntime
}

func NewManagedRuntimeCatalog(runtimes []ManagedRuntime) ManagedRuntimeCatalog {
	out := make([]ManagedRuntime, len(runtimes))
	copy(out, runtimes)
	return ManagedRuntimeCatalog{runtimes: out}
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
	if command[1] == "stop" || command[1] == "disable" || command[1] == "start" || command[1] == "enable" {
		unit := command[2]
		for _, prefix := range []string{"veil-caddy.service", "veil-hysteria2@", "veil-olcrtc@"} {
			if strings.HasPrefix(unit, prefix) && strings.HasSuffix(unit, ".service") {
				return true
			}
		}
	}
	for _, runtime := range c.runtimes {
		if runtime.PromotedVerb == "" {
			continue
		}
		if command[1] == runtime.PromotedVerb && matchUnit(command[2], runtime.Unit) {
			return true
		}
	}
	return false
}

func (c ManagedRuntimeCatalog) AllowsHealthUnit(unit string) bool {
	for _, runtime := range c.runtimes {
		if runtime.HealthCheckAfter && matchUnit(unit, runtime.Unit) {
			return true
		}
	}
	return false
}

func matchUnit(candidate, catalogUnit string) bool {
	if candidate == catalogUnit {
		return true
	}
	if idx := strings.Index(catalogUnit, "@"); idx != -1 {
		prefix := catalogUnit[:idx+1]
		suffix := catalogUnit[idx+1:]
		return strings.HasPrefix(candidate, prefix) && strings.HasSuffix(candidate, suffix)
	}
	return false
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
