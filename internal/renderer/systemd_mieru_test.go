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
		// mita (mieru's server binary) is a daemon controlled over its RPC socket:
		// start the daemon, then apply the generated config and start the proxy.
		"ExecStart=/usr/local/bin/mita run",
		"Environment=MITA_CONFIG_FILE=/var/lib/mita/server.conf.pb",
		"Environment=MITA_UDS_PATH=/var/lib/mita/mita.sock",
		"StateDirectory=mita",
		"/usr/local/bin/mita apply config /etc/veil/generated/mieru/server_config.json",
		"/usr/local/bin/mita start",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("Mieru unit missing %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "mieru run -c") {
		t.Fatalf("Mieru unit must not use the unsupported 'mieru run -c' invocation:\n%s", unit)
	}
}
