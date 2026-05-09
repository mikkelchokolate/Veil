package api

import (
	"path/filepath"
	"sort"

	"github.com/veil-panel/veil/internal/renderer"
)

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
	runtimes := []ManagedRuntime{{Name: "veil", ActionName: "veil", Unit: renderer.UnitVeil, ManualRestart: true}}
	ordered := []struct {
		Order   int
		Runtime ManagedRuntime
	}{}
	for _, capability := range NewProtocolCapabilityCatalog().All() {
		if capability.RuntimeUnit == "" {
			continue
		}
		ordered = append(ordered, struct {
			Order   int
			Runtime ManagedRuntime
		}{
			Order: capability.RuntimeOrder,
			Runtime: ManagedRuntime{
				Name:             capability.RuntimeName,
				ActionName:       capability.RuntimeActionName,
				Protocol:         capability.Protocol,
				Transport:        capability.RuntimeTransport,
				Unit:             capability.RuntimeUnit,
				PromotedSubpath:  capability.GeneratedConfig.Subpath,
				PromotedVerb:     capability.PromotedVerb,
				ManualRestart:    true,
				HealthCheckAfter: true,
			},
		})
	}
	ordered = append(ordered, struct {
		Order   int
		Runtime ManagedRuntime
	}{Order: 30, Runtime: ManagedRuntime{Name: "sing-box", ActionName: "sing-box", Unit: renderer.UnitWarp, PromotedSubpath: generatedWarpConfigSubpath, PromotedVerb: "reload", ManualRestart: true, HealthCheckAfter: true}})
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
	for _, item := range ordered {
		runtimes = append(runtimes, item.Runtime)
	}
	return ManagedRuntimeCatalog{runtimes: runtimes}
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
