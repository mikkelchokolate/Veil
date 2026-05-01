package renderer

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnits(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{
		VeilBinary:     "/usr/local/bin/veil",
		CaddyBinary:    "/usr/local/bin/caddy",
		HysteriaBinary: "/usr/local/bin/hysteria",
		SingBoxBinary:  "/usr/local/bin/sing-box",
		EtcDir:         "/etc/veil",
	})
	if len(units) != 4 {
		t.Fatalf("expected 4 units, got %d", len(units))
	}
	for _, name := range []string{"veil.service", "veil-naive.service", "veil-hysteria2.service", "veil-warp.service"} {
		if units[name] == "" {
			t.Fatalf("missing unit %s", name)
		}
	}
	if !strings.Contains(units["veil.service"], "ExecStart=/usr/local/bin/veil serve") {
		t.Fatalf("bad veil unit:\n%s", units["veil.service"])
	}
	if !strings.Contains(units["veil.service"], "EnvironmentFile=-/etc/veil/veil.env") {
		t.Fatalf("expected veil env file in unit:\n%s", units["veil.service"])
	}
	if !strings.Contains(units["veil-naive.service"], "/etc/veil/generated/caddy/Caddyfile") {
		t.Fatalf("bad naive unit:\n%s", units["veil-naive.service"])
	}
	if !strings.Contains(units["veil-hysteria2.service"], "/etc/veil/generated/hysteria2/server.yaml") {
		t.Fatalf("bad hysteria2 unit:\n%s", units["veil-hysteria2.service"])
	}
	if !strings.Contains(units["veil-warp.service"], "ExecStart=/usr/local/bin/sing-box run -c /etc/veil/generated/sing-box/warp.json") || !strings.Contains(units["veil-warp.service"], "ExecReload=/usr/local/bin/sing-box check -c /etc/veil/generated/sing-box/warp.json") {
		t.Fatalf("bad WARP unit:\n%s", units["veil-warp.service"])
	}
}
