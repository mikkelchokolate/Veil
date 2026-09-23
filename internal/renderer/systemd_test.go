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
	if !strings.Contains(helper, "ReadWritePaths=/opt/veil/etc /opt/veil/var /usr/local/bin /etc/ufw\n") {
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

func TestRenderSystemdUnitsQuotesExecStartWhenPathHasSpaces(t *testing.T) {
	units := RenderSystemdUnits(SystemdConfig{
		VeilBinary:  "/opt/Veil Panel/bin/veil",
		CaddyBinary: "/opt/Veil Panel/bin/caddy",
		EtcDir:      "/opt/Veil Panel/etc",
	})
	veil := units[UnitVeil]
	if !strings.Contains(veil, `ExecStart="/opt/Veil Panel/bin/veil" serve`) {
		t.Fatalf("veil.service must quote executable path with spaces:\n%s", veil)
	}
	if strings.Contains(veil, "ExecStart=/opt/Veil Panel/bin/veil serve") {
		t.Fatalf("unquoted ExecStart would be split by systemd:\n%s", veil)
	}
	if !strings.Contains(veil, `EnvironmentFile=-"/opt/Veil Panel/etc/veil.env"`) {
		t.Fatalf("EnvironmentFile must quote config path with spaces:\n%s", veil)
	}
	if !strings.Contains(veil, `ReadOnlyPaths="/opt/Veil Panel/etc"`) {
		t.Fatalf("ReadOnlyPaths must quote config path with spaces:\n%s", veil)
	}
	helper := units[UnitHelperService]
	if !strings.Contains(helper, `ExecStart="/opt/Veil Panel/bin/veil" helper serve --systemd-socket-activation`) {
		t.Fatalf("helper ExecStart must quote executable path with spaces:\n%s", helper)
	}
	backup := units[UnitBackupService]
	if !strings.Contains(backup, `ExecStart="/opt/Veil Panel/bin/veil" backup create`) {
		t.Fatalf("backup ExecStart must quote executable path with spaces:\n%s", backup)
	}
	if !strings.Contains(backup, `--key-path "/opt/Veil Panel/etc/state.key"`) {
		t.Fatalf("backup key path must be quoted:\n%s", backup)
	}
	caddy := units[UnitCaddy]
	if !strings.Contains(caddy, `ExecStart="/opt/Veil Panel/bin/caddy" run --config "/opt/Veil Panel/etc/generated/caddy/config.json"`) {
		t.Fatalf("caddy ExecStart must quote binary and config paths:\n%s", caddy)
	}

	defaults := RenderSystemdUnits(SystemdConfig{})
	if !strings.Contains(defaults[UnitVeil], "ExecStart=/usr/local/bin/veil serve") {
		t.Fatalf("default veil ExecStart should stay unquoted:\n%s", defaults[UnitVeil])
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
	// Internet-facing caddy shares the veil-proxy privilege boundary with the
	// other protocol units, not the panel account (issue #497, audit #506).
	if !strings.Contains(units["veil-caddy.service"], "User=veil-proxy\n") || !strings.Contains(units["veil-caddy.service"], "Group=veil-proxy\n") {
		t.Fatalf("caddy unit must run as veil-proxy:\n%s", units["veil-caddy.service"])
	}
	if strings.Contains(units["veil-caddy.service"], "User=veil\n") {
		t.Fatalf("caddy unit must not share the panel uid:\n%s", units["veil-caddy.service"])
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

// Issues #625/#626/#639/#650: on a packaged host a custom --etc-dir/--var-dir
// install must override every path-carrying directive the vendor units set —
// for ALL packaged service units, not just the panel/helper/backup trio. List
// directives (EnvironmentFile, ReadOnlyPaths, ReadWritePaths,
// InaccessiblePaths, ConditionPathExists) accumulate across drop-ins, so each
// must be cleared with an empty assignment before the custom value.
func TestRenderInstallDropInOverridesPackagedUnits(t *testing.T) {
	cfg := SystemdConfig{EtcDir: "/opt/veil/etc", VarDir: "/opt/veil/var"}

	t.Run("protocol units get ExecStart and InaccessiblePaths drop-ins", func(t *testing.T) {
		for name, want := range map[string][]string{
			UnitHysteria2: {"ExecStart=\n", "ExecStart=/usr/local/bin/hysteria server --config /opt/veil/etc/generated/hysteria2/%i.yaml"},
			UnitOlcrtc:    {"ExecStart=\n", "ExecStart=/usr/local/bin/olcrtc /opt/veil/etc/generated/olcrtc/%i.yaml"},
			UnitWarp:      {"ExecStart=\n", "ExecStart=/usr/local/bin/sing-box run -c /opt/veil/etc/generated/sing-box/warp.json", "ExecReload=\n", "ExecReload=/usr/local/bin/sing-box check -c /opt/veil/etc/generated/sing-box/warp.json"},
			UnitMieru:     {"ExecStart=\n", "ExecStart=/usr/local/bin/mita run", "ExecStartPost=\n", "/opt/veil/etc/generated/mieru/server_config.json", "ExecStop=\n", "ExecStop=/usr/local/bin/mita stop"},
		} {
			dropIn, ok := RenderInstallDropIn(name, cfg)
			if !ok {
				t.Fatalf("%s: expected install drop-in for custom paths", name)
			}
			for _, wantLine := range want {
				if !strings.Contains(dropIn, wantLine) {
					t.Fatalf("%s drop-in missing %q:\n%s", name, wantLine, dropIn)
				}
			}
			// The #615 mask must follow the custom VarDir instead of staying
			// stuck on the packaged default, and the abandoned default tree
			// stays masked as defense-in-depth.
			if !strings.Contains(dropIn, "InaccessiblePaths=\nInaccessiblePaths=/run/veil/helper.sock /opt/veil/var /var/lib/veil") {
				t.Fatalf("%s drop-in must reset InaccessiblePaths to the custom VarDir (keeping /var/lib/veil masked):\n%s", name, dropIn)
			}
		}
	})

	t.Run("caddy drop-in follows custom etc and masks custom var", func(t *testing.T) {
		dropIn, ok := RenderInstallDropIn(UnitCaddy, cfg)
		if !ok {
			t.Fatal("expected caddy drop-in")
		}
		for _, want := range []string{
			"ExecStart=\nExecStart=/usr/local/bin/caddy run --config /opt/veil/etc/generated/caddy/config.json",
			"ExecReload=\nExecReload=/usr/local/bin/caddy reload --config /opt/veil/etc/generated/caddy/config.json",
			"ReadOnlyPaths=\nReadOnlyPaths=/opt/veil/etc",
			"InaccessiblePaths=\nInaccessiblePaths=/run/veil/helper.sock /opt/veil/var /var/lib/veil",
		} {
			if !strings.Contains(dropIn, want) {
				t.Fatalf("caddy drop-in missing %q:\n%s", want, dropIn)
			}
		}
	})

	t.Run("backup drop-in resets the packaged condition and env file", func(t *testing.T) {
		dropIn, ok := RenderInstallDropIn(UnitBackupService, cfg)
		if !ok {
			t.Fatal("expected backup drop-in")
		}
		for _, want := range []string{
			"[Unit]\nConditionPathExists=\nConditionPathExists=/opt/veil/etc/backup.passphrase\n\n[Service]\n",
			"ExecStart=\nExecStart=/usr/local/bin/veil backup create --state /opt/veil/var/state.json --key-path /opt/veil/etc/state.key --passphrase-file /opt/veil/etc/backup.passphrase --output-dir /opt/veil/var/backups",
			"EnvironmentFile=\nEnvironmentFile=-/opt/veil/etc/veil.env",
			"ReadWritePaths=\nReadWritePaths=/opt/veil/var",
		} {
			if !strings.Contains(dropIn, want) {
				t.Fatalf("backup drop-in missing %q:\n%s", want, dropIn)
			}
		}
	})

	t.Run("panel and helper drop-ins clear packaged path grants", func(t *testing.T) {
		veil, ok := RenderInstallDropIn(UnitVeil, cfg)
		if !ok {
			t.Fatal("expected veil drop-in")
		}
		for _, want := range []string{
			"EnvironmentFile=\nEnvironmentFile=-/opt/veil/etc/veil.env",
			"ReadOnlyPaths=\nReadOnlyPaths=/opt/veil/etc",
			"ReadWritePaths=\nReadWritePaths=/opt/veil/var",
		} {
			if !strings.Contains(veil, want) {
				t.Fatalf("veil drop-in missing %q:\n%s", want, veil)
			}
		}
		helper, ok := RenderInstallDropIn(UnitHelperService, cfg)
		if !ok {
			t.Fatal("expected helper drop-in")
		}
		// Issue #663: the drop-in must match the packaged unit and the full
		// renderer — no writable /run path. The socket-activated helper
		// adopts FD3 and must never write the runtime dir holding its socket,
		// so the drop-in cannot reference /run/veil at all.
		if !strings.Contains(helper, "ReadWritePaths=\nReadWritePaths=/opt/veil/etc /opt/veil/var /usr/local/bin /etc/ufw\n") {
			t.Fatalf("helper drop-in must reset ReadWritePaths before the custom list:\n%s", helper)
		}
		if strings.Contains(helper, "/run/veil") {
			t.Fatalf("helper drop-in must not reference a writable runtime dir:\n%s", helper)
		}
	})

	t.Run("no drop-in without custom paths or for units without path overrides", func(t *testing.T) {
		if _, ok := RenderInstallDropIn(UnitVeil, SystemdConfig{}); ok {
			t.Fatal("default install must not render a drop-in")
		}
		for _, name := range []string{UnitHelperSocket, UnitBackupTimer} {
			if _, ok := RenderInstallDropIn(name, cfg); ok {
				t.Fatalf("%s has no path-carrying directives to override", name)
			}
		}
	})
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
	// The panel must not own /run/veil: veil-helper.socket creates it
	// root:root 0711 so no veil-uid process can replace the helper socket.
	if strings.Contains(panel, "RuntimeDirectory=") {
		t.Fatalf("veil.service must not claim a RuntimeDirectory (helper socket dir is root-owned):\n%s", panel)
	}
	helper := units[UnitHelperService]
	for _, want := range []string{
		"User=root",
		"ExecStart=/usr/local/bin/veil helper serve --systemd-socket-activation",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK",
		"CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW\n",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW",
		"Environment=\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"",
		"ReadWritePaths=/etc/veil /var/lib/veil /usr/local/bin /etc/ufw\n",
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
		"DirectoryMode=0711",
		"RemoveOnStop=true",
	} {
		if !strings.Contains(socket, want) {
			t.Fatalf("veil-helper.socket missing %q:\n%s", want, socket)
		}
	}
	caddy := units[UnitCaddy]
	for _, want := range []string{"User=veil-proxy\n", "Group=veil-proxy\n", "PrivateDevices=true", "ReadOnlyPaths=/etc/veil", "InaccessiblePaths=/run/veil/helper.sock /var/lib/veil"} {
		if !strings.Contains(caddy, want) {
			t.Fatalf("veil-caddy.service missing %q:\n%s", want, caddy)
		}
	}
	if strings.Contains(caddy, "User=veil\n") || strings.Contains(caddy, "Group=veil\n") {
		t.Fatalf("veil-caddy.service must not share the panel identity:\n%s", caddy)
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
	for _, name := range []string{UnitHysteria2, UnitOlcrtc, UnitWarp, UnitCaddy} {
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
	// veil-mieru.service runs as the dedicated veil-mita identity (issue #624):
	// the appctl UDS is a control plane, so it must not share the veil-proxy
	// uid/gid that every other edge unit uses.
	mieru := units[UnitMieru]
	for _, want := range []string{
		"User=veil-mita\n",
		"Group=veil-mita\n",
		"SupplementaryGroups=veil-proxy",
		"RuntimeDirectory=veil-mieru\n",
		"RuntimeDirectoryMode=0750",
		"UMask=0007",
	} {
		if !strings.Contains(mieru, want) {
			t.Fatalf("veil-mieru.service missing %q:\n%s", want, mieru)
		}
	}
	if strings.Contains(mieru, "User=veil-proxy") || strings.Contains(mieru, "Group=veil-proxy\n") {
		t.Fatalf("veil-mieru.service must not run as the shared veil-proxy identity:\n%s", mieru)
	}
	if strings.Contains(mieru, "User=veil\n") {
		t.Fatalf("veil-mieru.service must not share User=veil with veil.service:\n%s", mieru)
	}
	if strings.Contains(mieru, "ReadWritePaths=/var/lib/veil") || strings.Contains(mieru, "ReadWritePaths=/etc/veil") {
		t.Fatalf("veil-mieru.service must not remount Panel state writable:\n%s", mieru)
	}
	if !strings.Contains(mieru, "InaccessiblePaths=/run/veil/helper.sock /var/lib/veil") {
		t.Fatalf("veil-mieru.service missing helper/state InaccessiblePaths:\n%s", mieru)
	}
}
