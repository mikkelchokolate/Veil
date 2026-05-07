package renderer

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnitsIncludesMieruUnit(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{EtcDir: "/etc/veil"})
	unit := units["veil-mieru.service"]
	if unit == "" {
		t.Fatalf("missing veil-mieru.service: %+v", units)
	}
	for _, want := range []string{
		"Description=Veil managed Mieru",
		"ExecStart=/usr/local/bin/mieru run -c /etc/veil/generated/mieru/server_config.json",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("Mieru unit missing %q:\n%s", want, unit)
		}
	}
}
