package renderer_test

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func TestRenderedSystemdUnitsCoverProtocolRuntimeDescriptors(t *testing.T) {
	units := renderer.RenderSystemdUnits(renderer.SystemdConfig{})
	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		runtimeProvider, ok := protocols.AsRuntimeProvider(plugin)
		if !ok {
			continue
		}
		for _, descriptor := range runtimeProvider.RuntimeDescriptors(nil) {
			unit := descriptor.Unit
			if descriptor.TemplateUnit != "" {
				unit = descriptor.TemplateUnit
			}
			if unit == "" {
				t.Fatalf("%s runtime descriptor has no unit: %+v", plugin.Protocol(), descriptor)
			}
			if _, ok := units[unit]; !ok {
				t.Fatalf("%s runtime unit %s is not rendered by RenderSystemdUnits", plugin.Protocol(), unit)
			}
		}
	}
}
