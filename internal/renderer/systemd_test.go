package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackagingSystemdUnitsMatchDefaultRenderer(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})
	for name := range units {
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

func TestRenderedSystemdUnitsAvoidDuplicateSingletonDirectives(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})
	singletonDirectives := []string{
		"NoNewPrivileges",
		"ProtectSystem",
		"ProtectHome",
		"PrivateTmp",
		"CapabilityBoundingSet",
		"AmbientCapabilities",
		"RestrictAddressFamilies",
		"SystemCallArchitectures",
		"ProtectKernelTunables",
		"ProtectKernelModules",
		"ProtectControlGroups",
		"RestrictSUIDSGID",
		"LockPersonality",
		"RestrictRealtime",
		"MemoryDenyWriteExecute",
		"UMask",
	}
	for name, body := range units {
		for _, directive := range singletonDirectives {
			count := countSystemdDirective(body, directive)
			if count > 1 {
				t.Fatalf("%s repeats singleton directive %s %d times:\n%s", name, directive, count, body)
			}
		}
	}
}

func countSystemdDirective(body, directive string) int {
	prefix := directive + "="
	count := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}

func TestRenderSystemdUnitsPropagatesCustomEtcAndVarDir(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{EtcDir: "/opt/veil/etc", VarDir: "/opt/veil/var"})
	panel := units[UnitVeil]
	for _, want := range []string{
		"Environment=VEIL_STATE_PATH=/opt/veil/var/state.json",
		"Environment=VEIL_KEY_PATH=/opt/veil/etc/state.key",
		"Environment=VEIL_APPLY_ROOT=/opt/veil/var/staging",
		"Environment=VEIL_LIVE_ROOT=/opt/veil/etc/generated",
		"ReadOnlyPaths=/opt/veil/etc",
		"ReadWritePaths=/opt/veil/var",
	} {
		if !strings.Contains(panel, want) {
			t.Fatalf("veil.service missing %q:\n%s", want, panel)
		}
	}
	helper := units[UnitHelperService]
	if !strings.Contains(helper, "ReadWritePaths=/opt/veil/etc /opt/veil/var /usr/local/bin /etc/ufw /run /var/run") {
		t.Fatalf("helper ReadWritePaths should include custom trees:\n%s", helper)
	}
	backup := units[UnitBackupService]
	if !strings.Contains(backup, "--state /opt/veil/var/state.json") || !strings.Contains(backup, "--output-dir /opt/veil/var/backups") {
		t.Fatalf("backup unit should use custom state paths:\n%s", backup)
	}
	caddy := units[UnitCaddy]
	if !strings.Contains(caddy, "ReadOnlyPaths=/opt/veil/etc") {
		t.Fatalf("caddy ReadOnlyPaths should include custom etc:\n%s", caddy)
	}
	if strings.Contains(caddy, "ReadWritePaths=") {
		t.Fatalf("caddy unit must not remount custom trees writable:\n%s", caddy)
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
	for _, name := range []string{UnitVeil, UnitHelperService, UnitHelperSocket, UnitCaddy, UnitHysteria2, UnitOlcrtc, UnitWarp, UnitMieru, UnitBackupService, UnitBackupTimer} {
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
	if !strings.Contains(units["veil-caddy.service"], "/etc/veil/generated/caddy/config.json") {
		t.Fatalf("bad caddy unit:\n%s", units["veil-caddy.service"])
	}
	if !strings.Contains(units["veil-caddy.service"], "User=veil") || !strings.Contains(units["veil-caddy.service"], "Group=veil") {
		t.Fatalf("caddy unit must run as veil:\n%s", units["veil-caddy.service"])
	}
	if strings.Contains(units["veil-caddy.service"], "ReadWritePaths=") {
		t.Fatalf("caddy unit must not remount Veil state writable:\n%s", units["veil-caddy.service"])
	}
	if !strings.Contains(units["veil-caddy.service"], "PrivateDevices=true") {
		t.Fatalf("caddy unit must set PrivateDevices=true:\n%s", units["veil-caddy.service"])
	}
	if !strings.Contains(units["veil-hysteria2@.service"], "/etc/veil/generated/hysteria2/%i.yaml") {
		t.Fatalf("bad hysteria2 unit:\n%s", units["veil-hysteria2@.service"])
	}
	if !strings.Contains(units["veil-mieru.service"], "ExecStop=/usr/local/bin/mita stop") {
		t.Fatalf("mieru unit must stop mita gracefully on teardown:\n%s", units["veil-mieru.service"])
	}
	if !strings.Contains(units["veil-olcrtc@.service"], "/etc/veil/generated/olcrtc/%i.yaml") {
		t.Fatalf("bad olcrtc unit:\n%s", units["veil-olcrtc@.service"])
	}
	if !strings.Contains(units["veil-olcrtc@.service"], "StartLimitBurst=5") || !strings.Contains(units["veil-olcrtc@.service"], "StartLimitIntervalSec=30") {
		t.Fatalf("olcrtc unit must cap crash-loops:\n%s", units["veil-olcrtc@.service"])
	}
	if !strings.Contains(units["veil-warp.service"], "ExecStart=/usr/local/bin/sing-box run -c /etc/veil/generated/sing-box/warp.json") || !strings.Contains(units["veil-warp.service"], "ExecReload=/usr/local/bin/sing-box check -c /etc/veil/generated/sing-box/warp.json") {
		t.Fatalf("bad WARP unit:\n%s", units["veil-warp.service"])
	}
}

func TestRenderSystemdUnitsDefaults(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{})

	for _, name := range []string{UnitVeil, UnitHelperService, UnitHelperSocket, UnitCaddy, UnitHysteria2, UnitOlcrtc, UnitWarp, UnitMieru, UnitBackupService, UnitBackupTimer} {
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

	// veil-caddy.service: default CaddyBinary and EtcDir config path
	naiveUnit := units["veil-caddy.service"]
	if !strings.Contains(naiveUnit, "ExecStart=/usr/local/bin/caddy run --config /etc/veil/generated/caddy/config.json") {
		t.Fatalf("veil-caddy.service: expected default CaddyBinary and EtcDir, got:\n%s", naiveUnit)
	}
	if !strings.Contains(naiveUnit, "ExecReload=/usr/local/bin/caddy reload --config /etc/veil/generated/caddy/config.json") {
		t.Fatalf("veil-caddy.service: expected default CaddyBinary reload, got:\n%s", naiveUnit)
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
		"Environment=VEIL_STATE_PATH=/var/lib/veil/state.json",
		"Environment=VEIL_KEY_PATH=/etc/veil/state.key",
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
		"RestrictAddressFamilies=AF_UNIX AF_NETLINK",
		"CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW\n",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW",
		"Environment=\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"",
		"ReadWritePaths=/etc/veil /var/lib/veil /usr/local/bin /etc/ufw /run /var/run",
	} {
		if !strings.Contains(helper, want) {
			t.Fatalf("veil-helper.service missing %q:\n%s", want, helper)
		}
	}
	if strings.Contains(helper, "ReadOnlyPaths=/etc/veil") {
		t.Fatalf("veil-helper.service must write sibling restore/rotate temps next to state.key:\n%s", helper)
	}
	for _, forbid := range []string{
		"PrivateNetwork",
		"NetworkNamespacePath",
		"JoinsNamespaceOf",
	} {
		if strings.Contains(helper, forbid) {
			t.Fatalf("veil-helper.service must not contain network-isolation directive %q:\n%s", forbid, helper)
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
	caddy := units[UnitCaddy]
	for _, want := range []string{"User=veil\n", "Group=veil\n", "PrivateDevices=true", "ReadOnlyPaths=/etc/veil"} {
		if !strings.Contains(caddy, want) {
			t.Fatalf("veil-caddy.service missing %q:\n%s", want, caddy)
		}
	}
	if strings.Contains(caddy, "ReadWritePaths=") {
		t.Fatalf("veil-caddy.service must not remount Veil paths writable:\n%s", caddy)
	}
	for _, name := range []string{UnitHysteria2, UnitOlcrtc, UnitWarp, UnitMieru, UnitCaddy} {
		unit := units[name]
		if !strings.Contains(unit, "User=") {
			t.Fatalf("%s is missing User=:\n%s", name, unit)
		}
	}
	for _, name := range []string{UnitHysteria2, UnitOlcrtc, UnitWarp, UnitMieru} {
		unit := units[name]
		if !strings.Contains(unit, "User=veil-proxy") || !strings.Contains(unit, "Group=veil-proxy") {
			t.Fatalf("%s must run as veil-proxy:\n%s", name, unit)
		}
		if strings.Contains(unit, "User=veil\n") {
			t.Fatalf("%s must not share User=veil with veil.service:\n%s", name, unit)
		}
		if strings.Contains(unit, "ReadWritePaths=/var/lib/veil") || strings.Contains(unit, "ReadWritePaths=/etc/veil") {
			t.Fatalf("%s must not remount Panel state writable:\n%s", name, unit)
		}
		if !strings.Contains(unit, "InaccessiblePaths=/run/veil/helper.sock /var/lib/veil") {
			t.Fatalf("%s missing helper/state InaccessiblePaths:\n%s", name, unit)
		}
	}
}
