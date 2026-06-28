package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSystemdUnitNamesMatchRenderer(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})
	names := ManagedSystemdUnitNames()
	if len(names) != len(units) {
		t.Fatalf("managed unit names = %d, rendered units = %d", len(names), len(units))
	}
	for _, name := range names {
		if units[name] == "" {
			t.Fatalf("managed unit %s missing from renderer", name)
		}
	}
}

func TestPackagingSystemdUnitsMatchDefaultRenderer(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})
	for _, name := range ManagedSystemdUnitNames() {
		body, err := os.ReadFile(filepath.Join("..", "..", "packaging", "systemd", name))
		if err != nil {
			t.Fatalf("read packaging unit %s: %v", name, err)
		}
		bodyStr := strings.ReplaceAll(string(body), "\r\n", "\n")
		unitsStr := strings.ReplaceAll(units[name], "\r\n", "\n")
		if bodyStr != unitsStr {
			t.Fatalf("packaging unit %s drifted from default renderer\n--- packaging ---\n%s\n--- renderer ---\n%s", name, bodyStr, unitsStr)
		}
	}
}

func TestRenderSystemdUnits(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{
		VeilBinary:     "/usr/local/bin/veil",
		CaddyBinary:    "/usr/local/bin/caddy",
		HysteriaBinary: "/usr/local/bin/hysteria",
		SingBoxBinary:  "/usr/local/bin/sing-box",
		EtcDir:         "/etc/veil",
	})
	if len(units) != len(ManagedSystemdUnitNames()) {
		t.Fatalf("expected %d units, got %d", len(ManagedSystemdUnitNames()), len(units))
	}
	for _, name := range ManagedSystemdUnitNames() {
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
	if !strings.Contains(units["veil-caddy@.service"], "/etc/veil/generated/caddy/%i.Caddyfile") {
		t.Fatalf("bad caddy unit:\n%s", units["veil-caddy@.service"])
	}
	if !strings.Contains(units["veil-hysteria2@.service"], "/etc/veil/generated/hysteria2/%i.yaml") {
		t.Fatalf("bad hysteria2 unit:\n%s", units["veil-hysteria2@.service"])
	}
	if !strings.Contains(units["veil-olcrtc@.service"], "/etc/veil/generated/olcrtc/%i.yaml") {
		t.Fatalf("bad olcrtc unit:\n%s", units["veil-olcrtc@.service"])
	}
	if !strings.Contains(units["veil-warp.service"], "ExecStart=/usr/local/bin/sing-box run -c /etc/veil/generated/sing-box/warp.json") || !strings.Contains(units["veil-warp.service"], "ExecReload=/usr/local/bin/sing-box check -c /etc/veil/generated/sing-box/warp.json") {
		t.Fatalf("bad WARP unit:\n%s", units["veil-warp.service"])
	}
}

func TestRenderSystemdUnitsDefaults(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})

	if len(units) != len(ManagedSystemdUnitNames()) {
		t.Fatalf("expected %d units, got %d", len(ManagedSystemdUnitNames()), len(units))
	}

	for _, name := range ManagedSystemdUnitNames() {
		if units[name] == "" {
			t.Fatalf("missing unit %s", name)
		}
	}

	// veil.service: default VeilBinary and EtcDir
	veilUnit := units["veil.service"]
	if !strings.Contains(veilUnit, "ExecStart=/usr/local/bin/veil serve") {
		t.Fatalf("veil.service: expected default VeilBinary, got:\n%s", veilUnit)
	}
	if !strings.Contains(veilUnit, "EnvironmentFile=-/etc/veil/veil.env") {
		t.Fatalf("veil.service: expected default EtcDir env file, got:\n%s", veilUnit)
	}

	// veil-caddy@.service: default CaddyBinary and EtcDir config path
	naiveUnit := units["veil-caddy@.service"]
	if !strings.Contains(naiveUnit, "ExecStart=/usr/local/bin/caddy run --config /etc/veil/generated/caddy/%i.Caddyfile") {
		t.Fatalf("veil-caddy@.service: expected default CaddyBinary and EtcDir, got:\n%s", naiveUnit)
	}
	if !strings.Contains(naiveUnit, "ExecReload=/usr/local/bin/caddy reload --config /etc/veil/generated/caddy/%i.Caddyfile") {
		t.Fatalf("veil-caddy@.service: expected default CaddyBinary reload, got:\n%s", naiveUnit)
	}

	// veil-hysteria2@.service: default HysteriaBinary and EtcDir config path
	hysteriaUnit := units["veil-hysteria2@.service"]
	if !strings.Contains(hysteriaUnit, "ExecStart=/usr/local/bin/hysteria server --config /etc/veil/generated/hysteria2/%i.yaml") {
		t.Fatalf("veil-hysteria2@.service: expected default HysteriaBinary and EtcDir, got:\n%s", hysteriaUnit)
	}

	// veil-olcrtc@.service: default OlcrtcBinary and EtcDir config path
	olcrtcUnit := units["veil-olcrtc@.service"]
	if !strings.Contains(olcrtcUnit, "ExecStart=/usr/local/bin/olcrtc /etc/veil/generated/olcrtc/%i.yaml") {
		t.Fatalf("veil-olcrtc@.service: expected default OlcrtcBinary and EtcDir, got:\n%s", olcrtcUnit)
	}

	// veil-warp.service: default SingBoxBinary and EtcDir config path
	warpUnit := units["veil-warp.service"]
	if !strings.Contains(warpUnit, "ExecStart=/usr/local/bin/sing-box run -c /etc/veil/generated/sing-box/warp.json") {
		t.Fatalf("veil-warp.service: expected default SingBoxBinary start, got:\n%s", warpUnit)
	}
	if !strings.Contains(warpUnit, "ExecReload=/usr/local/bin/sing-box check -c /etc/veil/generated/sing-box/warp.json") {
		t.Fatalf("veil-warp.service: expected default SingBoxBinary reload, got:\n%s", warpUnit)
	}
}

func TestPanelAndHelperUnitsEnforcePrivilegeBoundary(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})
	panel := units[UnitVeil]
	for _, want := range []string{
		"User=veil",
		"Group=veil",
		"Requires=veil-helper.socket",
		"Environment=VEIL_HELPER_SOCKET=/run/veil/helper.sock",
		"Environment=VEIL_APPLY_ROOT=/var/lib/veil/staging",
		"Environment=VEIL_LIVE_ROOT=/etc/veil/generated",
		"CapabilityBoundingSet=\n",
		"AmbientCapabilities=\n",
		"ReadOnlyPaths=/etc/veil",
		"ReadWritePaths=/var/lib/veil",
	} {
		if !strings.Contains(panel, want) {
			t.Fatalf("veil.service missing %q:\n%s", want, panel)
		}
	}
	helper := units[UnitHelperService]
	for _, want := range []string{
		"User=root",
		"ExecStart=/usr/local/bin/veil helper serve --systemd-socket-activation",
		"PrivateNetwork=true",
		"RestrictAddressFamilies=AF_UNIX AF_NETLINK",
		"CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW\n",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW",
		"Environment=\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"",
		"ReadWritePaths=/etc/veil/generated /etc/veil/certs /etc/veil/state.key /var/lib/veil /usr/local/bin",
	} {
		if !strings.Contains(helper, want) {
			t.Fatalf("veil-helper.service missing %q:\n%s", want, helper)
		}
	}
	socket := units[UnitHelperSocket]
	for _, want := range []string{
		"ListenStream=/run/veil/helper.sock",
		"SocketUser=root",
		"SocketGroup=veil",
		"SocketMode=0660",
		"RemoveOnStop=true",
	} {
		if !strings.Contains(socket, want) {
			t.Fatalf("veil-helper.socket missing %q:\n%s", want, socket)
		}
	}
}
