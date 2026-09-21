package renderer

import (
	"path"
	"strings"
)

const (
	UnitVeil          = "veil.service"
	UnitCaddy         = "veil-caddy.service"
	UnitHysteria2     = "veil-hysteria2@.service"
	UnitOlcrtc        = "veil-olcrtc@.service"
	UnitWarp          = "veil-warp.service"
	UnitMieru         = "veil-mieru.service"
	UnitBackupService = "veil-backup.service"
	UnitBackupTimer   = "veil-backup.timer"
	UnitHelperService = "veil-helper.service"
	UnitHelperSocket  = "veil-helper.socket"
)

const systemdHardeningBlock = `CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
MemoryDenyWriteExecute=true
UMask=0077
`

type SystemdConfig struct {
	VeilBinary     string
	CaddyBinary    string
	HysteriaBinary string
	SingBoxBinary  string
	MieruBinary    string
	OlcrtcBinary   string
	EtcDir         string
	VarDir         string
}

var systemdHardeningBlockOlcrtc = strings.Replace(
	systemdHardeningBlock,
	"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
	"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK",
	1,
)

// mita's appctl UDS must stay connectable by the veil panel: connecting to a
// unix socket needs write permission, so the daemon creates it group-writable
// for veil-proxy (the panel account is a supplementary veil-proxy member).
// UMask 0007 deliberately widens every file mita creates under its
// RuntimeDirectory/StateDirectory to group scope — acceptable because the
// panel is already in veil-proxy — and keeps world access at none.
var systemdHardeningBlockMieru = strings.Replace(
	systemdHardeningBlock,
	"UMask=0077",
	"# appctl UDS stays group-writable so the veil panel (supplementary\n# veil-proxy member) can connect; unix connect needs write on the socket.\nUMask=0007",
	1,
)

func systemdQuote(p string) string {
	if p == "" || !strings.ContainsAny(p, " \t\"'\\") {
		return p
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p)
	return `"` + escaped + `"`
}

func systemdAssign(key, value string) string {
	if value == "" || !strings.ContainsAny(value, " \t\"'\\") {
		return key + "=" + value
	}
	return key + "=" + systemdQuote(value)
}

// defaultSystemdConfig fills unset fields with the packaged defaults shared
// by the full unit render and the install drop-ins.
func defaultSystemdConfig(cfg SystemdConfig) SystemdConfig {
	if cfg.VeilBinary == "" {
		cfg.VeilBinary = "/usr/local/bin/veil"
	}
	if cfg.CaddyBinary == "" {
		cfg.CaddyBinary = "/usr/local/bin/caddy"
	}
	if cfg.HysteriaBinary == "" {
		cfg.HysteriaBinary = "/usr/local/bin/hysteria"
	}
	if cfg.SingBoxBinary == "" {
		cfg.SingBoxBinary = "/usr/local/bin/sing-box"
	}
	if cfg.MieruBinary == "" {
		cfg.MieruBinary = "/usr/local/bin/mita"
	}
	if cfg.OlcrtcBinary == "" {
		cfg.OlcrtcBinary = "/usr/local/bin/olcrtc"
	}
	if cfg.EtcDir == "" {
		cfg.EtcDir = "/etc/veil"
	}
	if cfg.VarDir == "" {
		cfg.VarDir = "/var/lib/veil"
	}
	return cfg
}

func RenderSystemdUnits(cfg SystemdConfig) map[string]string {
	cfg = defaultSystemdConfig(cfg)
	applyRoot := path.Join(cfg.VarDir, "staging")
	statePath := path.Join(cfg.VarDir, "state.json")
	keyPath := path.Join(cfg.EtcDir, "state.key")
	backupDir := path.Join(cfg.VarDir, "backups")
	veilBin := systemdQuote(cfg.VeilBinary)
	caddyBin := systemdQuote(cfg.CaddyBinary)
	hysteriaBin := systemdQuote(cfg.HysteriaBinary)
	singBoxBin := systemdQuote(cfg.SingBoxBinary)
	mieruBin := systemdQuote(cfg.MieruBinary)
	olcrtcBin := systemdQuote(cfg.OlcrtcBinary)
	etcDir := systemdQuote(cfg.EtcDir)
	varDir := systemdQuote(cfg.VarDir)
	envFile := systemdQuote(path.Join(cfg.EtcDir, "veil.env"))
	caddyConfig := systemdQuote(path.Join(cfg.EtcDir, "generated", "caddy", "config.json"))
	hysteriaConfig := systemdQuote(path.Join(cfg.EtcDir, "generated", "hysteria2", "%i.yaml"))
	olcrtcConfig := systemdQuote(path.Join(cfg.EtcDir, "generated", "olcrtc", "%i.yaml"))
	warpConfig := systemdQuote(path.Join(cfg.EtcDir, "generated", "sing-box", "warp.json"))
	mieruConfig := systemdQuote(path.Join(cfg.EtcDir, "generated", "mieru", "server_config.json"))
	stateKey := systemdQuote(keyPath)
	passphraseFile := systemdQuote(path.Join(cfg.EtcDir, "backup.passphrase"))
	quotedState := systemdQuote(statePath)
	quotedBackupDir := systemdQuote(backupDir)
	return map[string]string{
		UnitVeil: `[Unit]
Description=Veil panel
After=network-online.target veil-helper.socket
Wants=network-online.target
Requires=veil-helper.socket

[Service]
Type=simple
User=veil
Group=veil
# The panel must not own /run/veil: veil-helper.socket owns it root:root 0711
# so no veil-uid process can replace or shadow the helper socket. The panel
# only needs to connect to the socket, which the group-owned node allows.
EnvironmentFile=-` + envFile + `
Environment=VEIL_HELPER_SOCKET=/run/veil/helper.sock
Environment=` + systemdAssign("VEIL_STATE_PATH", statePath) + `
Environment=` + systemdAssign("VEIL_KEY_PATH", keyPath) + `
Environment=` + systemdAssign("VEIL_APPLY_ROOT", applyRoot) + `
Environment=` + systemdAssign("VEIL_LIVE_ROOT", path.Join(cfg.EtcDir, "generated")) + `
ExecStart=` + veilBin + ` serve
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
PrivateDevices=true
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
MemoryDenyWriteExecute=true
UMask=0077
ReadOnlyPaths=` + etcDir + `
ReadWritePaths=` + varDir + `

[Install]
WantedBy=multi-user.target
`,
		// Restore and key rotation write sibling temps next to state.key.
		// Allowing only the key file itself is EROFS under ProtectSystem=strict.
		UnitHelperService: `[Unit]
Description=Veil privileged helper
Requires=veil-helper.socket
After=veil-helper.socket

[Service]
Type=simple
User=root
Group=root
ExecStart=` + veilBin + ` helper serve --systemd-socket-activation
NoNewPrivileges=true
PrivateDevices=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=yes
CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
SystemCallArchitectures=native
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
MemoryDenyWriteExecute=true
UMask=0077
Environment="PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
Environment=` + systemdAssign("VEIL_STATE_PATH", statePath) + `
Environment=` + systemdAssign("VEIL_KEY_PATH", keyPath) + `
Environment=` + systemdAssign("VEIL_APPLY_ROOT", applyRoot) + `
Environment=` + systemdAssign("VEIL_LIVE_ROOT", path.Join(cfg.EtcDir, "generated")) + `
ReadWritePaths=` + etcDir + ` ` + varDir + ` /usr/local/bin /etc/ufw
`,
		UnitHelperSocket: `[Unit]
Description=Veil privileged helper socket

[Socket]
ListenStream=/run/veil/helper.sock
Accept=no
SocketUser=root
SocketGroup=veil
SocketMode=0660
# The socket parent stays root-owned and traverse-only (0711): the panel needs
# execute/search to connect but must never be able to list or replace entries
# in the helper socket directory.
DirectoryMode=0711
RemoveOnStop=true

[Install]
WantedBy=sockets.target
`,
		UnitCaddy: `[Unit]
Description=Veil managed NaiveProxy/Caddy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# Caddy is Internet-facing and must not share the panel's uid: it runs as
# veil-proxy like the protocol units so a compromised edge process cannot
# reach the root helper or the panel-owned state (audit #506).
User=veil-proxy
Group=veil-proxy
# Caddy binds :80/:443 with CAP_NET_BIND_SERVICE and reads 0640
# root:veil-proxy config under /etc/veil/generated and /etc/veil/tls. ACME
# material stays in StateDirectory=caddy (/var/lib/caddy); /etc/veil stays
# read-only while /var/lib/veil and the helper socket are fully unreachable.
StateDirectory=caddy
Environment=HOME=/var/lib/caddy XDG_DATA_HOME=/var/lib/caddy XDG_CONFIG_HOME=/var/lib/caddy
ExecStart=` + caddyBin + ` run --config ` + caddyConfig + `
ExecReload=` + caddyBin + ` reload --config ` + caddyConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
PrivateDevices=true
` + systemdHardeningBlock + `ReadOnlyPaths=` + etcDir + `
InaccessiblePaths=/run/veil/helper.sock ` + varDir + `

[Install]
WantedBy=multi-user.target
`,
		UnitHysteria2: `[Unit]
Description=Veil managed Hysteria2 (%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=veil-proxy
Group=veil-proxy
ExecStart=` + hysteriaBin + ` server --config ` + hysteriaConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `InaccessiblePaths=/run/veil/helper.sock ` + varDir + `

[Install]
WantedBy=multi-user.target
`,
		UnitOlcrtc: `[Unit]
Description=Veil managed olcRTC (%i)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=30
StartLimitBurst=5

[Service]
Type=simple
User=veil-proxy
Group=veil-proxy
ExecStart=` + olcrtcBin + ` ` + olcrtcConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlockOlcrtc + `InaccessiblePaths=/run/veil/helper.sock ` + varDir + `

[Install]
WantedBy=multi-user.target
`,
		UnitWarp: `[Unit]
Description=Veil managed WARP/sing-box outbound
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=veil-proxy
Group=veil-proxy
ExecStart=` + singBoxBin + ` run -c ` + warpConfig + `
ExecReload=` + singBoxBin + ` check -c ` + warpConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
SystemCallArchitectures=native
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
MemoryDenyWriteExecute=true
UMask=0077
InaccessiblePaths=/run/veil/helper.sock ` + varDir + `

[Install]
WantedBy=multi-user.target
`,
		UnitMieru: `[Unit]
Description=Veil managed Mieru
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=veil-proxy
Group=veil-proxy
Environment=MITA_CONFIG_FILE=/run/veil-mieru/server.conf.pb
Environment=MITA_UDS_PATH=/run/veil-mieru/mita.sock
Environment=MITA_INSECURE_UDS=1
Environment=MITA_LOG_NO_TIMESTAMP=true
RuntimeDirectory=veil-mieru
StateDirectory=mita
ExecStart=` + mieruBin + ` run
ExecStartPost=` + mieruActivationExecStartPost(mieruBin, mieruConfig) + `
ExecStop=` + mieruBin + ` stop
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlockMieru + `InaccessiblePaths=/run/veil/helper.sock ` + varDir + `

[Install]
WantedBy=multi-user.target
`,
		UnitBackupService: `[Unit]
Description=Veil encrypted state backup
Documentation=https://github.com/mikkelchokolate/Veil/blob/main/docs/disaster-recovery.md
ConditionPathExists=` + passphraseFile + `
After=local-fs.target

[Service]
Type=oneshot
EnvironmentFile=-` + envFile + `
ExecStart=` + veilBin + ` backup create --state ` + quotedState + ` --key-path ` + stateKey + ` --passphrase-file ` + passphraseFile + ` --output-dir ` + quotedBackupDir + ` --prune --daily 7 --weekly 4 --monthly 12
User=root
Group=root
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
PrivateDevices=true
CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH
RestrictAddressFamilies=AF_UNIX
SystemCallArchitectures=native
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
MemoryDenyWriteExecute=true
UMask=0077
ReadWritePaths=` + varDir + `
`,
		UnitBackupTimer: `[Unit]
Description=Daily Veil encrypted state backup
Documentation=https://github.com/mikkelchokolate/Veil/blob/main/docs/disaster-recovery.md

[Timer]
OnCalendar=*-*-* 02:00:00
RandomizedDelaySec=30m
Persistent=true
Unit=veil-backup.service

[Install]
WantedBy=timers.target
`,
	}
}

func UsesCustomInstallPaths(cfg SystemdConfig) bool {
	if cfg.EtcDir != "" && cfg.EtcDir != "/etc/veil" {
		return true
	}
	if cfg.VarDir != "" && cfg.VarDir != "/var/lib/veil" {
		return true
	}
	if cfg.VeilBinary != "" && cfg.VeilBinary != "/usr/local/bin/veil" {
		return true
	}
	if cfg.CaddyBinary != "" && cfg.CaddyBinary != "/usr/local/bin/caddy" {
		return true
	}
	return false
}

func RenderInstallDropIn(name string, cfg SystemdConfig) (string, bool) {
	if !UsesCustomInstallPaths(cfg) {
		return "", false
	}
	cfg = defaultSystemdConfig(cfg)
	var b strings.Builder
	if unit := dropInUnitOverrides(name, cfg); unit != "" {
		b.WriteString("[Unit]\n" + unit + "\n")
	}
	service := dropInServiceOverrides(name, cfg)
	if service == "" {
		return "", false
	}
	b.WriteString("[Service]\n" + service)
	return b.String(), true
}

// dropInUnitOverrides renders [Unit]-section resets for the packaged install
// drop-in. The vendor veil-backup.service carries
// ConditionPathExists=/etc/veil/backup.passphrase, which would silently skip
// the oneshot on a custom --etc-dir install; reset and re-point it at the
// custom passphrase file (issue #626).
func dropInUnitOverrides(name string, cfg SystemdConfig) string {
	if name != UnitBackupService {
		return ""
	}
	return "ConditionPathExists=\nConditionPathExists=" + systemdQuote(path.Join(cfg.EtcDir, "backup.passphrase")) + "\n"
}

// dropInInaccessiblePaths resets the packaged helper-socket/state mask and
// re-points it at the configured VarDir. The packaged /var/lib/veil stays
// masked alongside a custom VarDir: an abandoned tree can still hold panel
// state that veil-proxy units must never read (issues #615, #625).
func dropInInaccessiblePaths(varDir string) string {
	masked := "/run/veil/helper.sock " + systemdQuote(varDir)
	if varDir != "/var/lib/veil" {
		masked += " /var/lib/veil"
	}
	return "InaccessiblePaths=\nInaccessiblePaths=" + masked + "\n"
}

// mieruActivationExecStartPost is the ExecStartPost one-liner that waits for
// the mita RPC socket, applies the generated config, and starts the daemon.
// Shared by the full unit render and the install drop-in so the packaged unit
// override cannot drift from it (issue #625).
func mieruActivationExecStartPost(mieruBin, mieruConfig string) string {
	return "/bin/sh -c 'i=0; while [ $$i -lt 50 ]; do if [ -S /run/veil-mieru/mita.sock ]; then " + mieruBin + " apply config " + mieruConfig + " && " + mieruBin + " start && exit 0; fi; i=$$((i+1)); sleep 0.2; done; echo \"mita activation timed out\" >&2; exit 1'"
}

// dropInServiceOverrides renders the [Service]-section overrides a packaged
// unit needs when Veil is installed with custom paths. Every list-valued
// directive the vendor unit sets is cleared first (`Key=`): systemd.exec
// accumulates EnvironmentFile/ReadOnlyPaths/ReadWritePaths/InaccessiblePaths
// across fragments, so without the empty assignment the packaged default
// trees stay granted beside the custom ones (issues #639, #650). ExecStart
// and friends are reset before their replacement the same way.
func dropInServiceOverrides(name string, cfg SystemdConfig) string {
	var b strings.Builder
	veilBin := systemdQuote(cfg.VeilBinary)
	envFile := systemdQuote(path.Join(cfg.EtcDir, "veil.env"))
	writePanelEnvironment := func() {
		b.WriteString("Environment=" + systemdAssign("VEIL_STATE_PATH", path.Join(cfg.VarDir, "state.json")) + "\n")
		b.WriteString("Environment=" + systemdAssign("VEIL_KEY_PATH", path.Join(cfg.EtcDir, "state.key")) + "\n")
		b.WriteString("Environment=" + systemdAssign("VEIL_APPLY_ROOT", path.Join(cfg.VarDir, "staging")) + "\n")
		b.WriteString("Environment=" + systemdAssign("VEIL_LIVE_ROOT", path.Join(cfg.EtcDir, "generated")) + "\n")
	}
	switch name {
	case UnitVeil:
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + veilBin + " serve\n")
		b.WriteString("EnvironmentFile=\n")
		b.WriteString("EnvironmentFile=-" + envFile + "\n")
		writePanelEnvironment()
		b.WriteString("ReadOnlyPaths=\n")
		b.WriteString("ReadOnlyPaths=" + systemdQuote(cfg.EtcDir) + "\n")
		b.WriteString("ReadWritePaths=\n")
		b.WriteString("ReadWritePaths=" + systemdQuote(cfg.VarDir) + "\n")
	case UnitHelperService:
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + veilBin + " helper serve --systemd-socket-activation\n")
		writePanelEnvironment()
		b.WriteString("ReadWritePaths=\n")
		b.WriteString("ReadWritePaths=" + systemdQuote(cfg.EtcDir) + " " + systemdQuote(cfg.VarDir) + " /usr/local/bin /etc/ufw /run/veil\n")
	case UnitBackupService:
		passphraseFile := systemdQuote(path.Join(cfg.EtcDir, "backup.passphrase"))
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + veilBin + " backup create --state " + systemdQuote(path.Join(cfg.VarDir, "state.json")) + " --key-path " + systemdQuote(path.Join(cfg.EtcDir, "state.key")) + " --passphrase-file " + passphraseFile + " --output-dir " + systemdQuote(path.Join(cfg.VarDir, "backups")) + " --prune --daily 7 --weekly 4 --monthly 12\n")
		b.WriteString("EnvironmentFile=\n")
		b.WriteString("EnvironmentFile=-" + envFile + "\n")
		b.WriteString("ReadWritePaths=\n")
		b.WriteString("ReadWritePaths=" + systemdQuote(cfg.VarDir) + "\n")
	case UnitCaddy:
		config := systemdQuote(path.Join(cfg.EtcDir, "generated", "caddy", "config.json"))
		caddyBin := systemdQuote(cfg.CaddyBinary)
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + caddyBin + " run --config " + config + "\n")
		b.WriteString("ExecReload=\n")
		b.WriteString("ExecReload=" + caddyBin + " reload --config " + config + "\n")
		b.WriteString("ReadOnlyPaths=\n")
		b.WriteString("ReadOnlyPaths=" + systemdQuote(cfg.EtcDir) + "\n")
		b.WriteString(dropInInaccessiblePaths(cfg.VarDir))
	case UnitHysteria2:
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + systemdQuote(cfg.HysteriaBinary) + " server --config " + systemdQuote(path.Join(cfg.EtcDir, "generated", "hysteria2", "%i.yaml")) + "\n")
		b.WriteString(dropInInaccessiblePaths(cfg.VarDir))
	case UnitOlcrtc:
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + systemdQuote(cfg.OlcrtcBinary) + " " + systemdQuote(path.Join(cfg.EtcDir, "generated", "olcrtc", "%i.yaml")) + "\n")
		b.WriteString(dropInInaccessiblePaths(cfg.VarDir))
	case UnitWarp:
		config := systemdQuote(path.Join(cfg.EtcDir, "generated", "sing-box", "warp.json"))
		singBoxBin := systemdQuote(cfg.SingBoxBinary)
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + singBoxBin + " run -c " + config + "\n")
		b.WriteString("ExecReload=\n")
		b.WriteString("ExecReload=" + singBoxBin + " check -c " + config + "\n")
		b.WriteString(dropInInaccessiblePaths(cfg.VarDir))
	case UnitMieru:
		mieruBin := systemdQuote(cfg.MieruBinary)
		mieruConfig := systemdQuote(path.Join(cfg.EtcDir, "generated", "mieru", "server_config.json"))
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + mieruBin + " run\n")
		b.WriteString("ExecStartPost=\n")
		b.WriteString("ExecStartPost=" + mieruActivationExecStartPost(mieruBin, mieruConfig) + "\n")
		b.WriteString("ExecStop=\n")
		b.WriteString("ExecStop=" + mieruBin + " stop\n")
		b.WriteString(dropInInaccessiblePaths(cfg.VarDir))
	}
	return b.String()
}
