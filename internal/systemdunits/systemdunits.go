package systemdunits

import (
	"sort"

	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// Names returns the complete set of managed systemd unit names that Veil
// installs and uninstalls. The list combines the core Veil units with the
// runtime unit templates exposed by each registered protocol plugin.
func Names() []string {
	names := []string{
		renderer.UnitVeil,
		renderer.UnitHelperService,
		renderer.UnitHelperSocket,
		renderer.UnitBackupService,
		renderer.UnitBackupTimer,
		renderer.UnitWarp,
	}
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
	}

	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		rp, ok := protocols.AsRuntimeProvider(plugin)
		if !ok {
			continue
		}
		for _, runtime := range rp.RuntimeDescriptors(nil) {
			unit := runtime.Unit
			if runtime.TemplateUnit != "" {
				unit = runtime.TemplateUnit
			}
			if unit == "" || seen[unit] {
				continue
			}
			seen[unit] = true
			names = append(names, unit)
		}
	}

	sort.Strings(names)
	return names
}

// Render returns the rendered systemd unit content for all managed units.
func Render(cfg renderer.SystemdConfig) map[string]string {
	all := renderer.RenderSystemdUnits(cfg)
	out := make(map[string]string, len(all))
	for _, name := range Names() {
		if content, ok := all[name]; ok {
			out[name] = content
		}
	}
	return out
}
