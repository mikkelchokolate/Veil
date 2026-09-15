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

func RenderSystemdUnits(cfg SystemdConfig) map[string]string {
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
	applyRoot := path.Join(cfg.VarDir, "staging")
	statePath := path.Join(cfg.VarDir, "state.json")
	keyPath := path.Join(cfg.EtcDir, "state.key")
	backupDir := path.Join(cfg.VarDir, "backups")
	caddyConfig := path.Join(cfg.EtcDir, "generated", "caddy", "config.json")
	hysteriaConfig := path.Join(cfg.EtcDir, "generated", "hysteria2", "%i.yaml")
	olcrtcConfig := path.Join(cfg.EtcDir, "generated", "olcrtc", "%i.yaml")
	warpConfig := path.Join(cfg.EtcDir, "generated", "sing-box", "warp.json")
	mieruConfig := path.Join(cfg.EtcDir, "generated", "mieru", "server_config.json")
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
RuntimeDirectory=veil
RuntimeDirectoryMode=0750
RuntimeDirectoryPreserve=yes
EnvironmentFile=-` + path.Join(cfg.EtcDir, "veil.env") + `
Environment=VEIL_HELPER_SOCKET=/run/veil/helper.sock
Environment=VEIL_STATE_PATH=` + statePath + `
Environment=VEIL_KEY_PATH=` + keyPath + `
Environment=VEIL_APPLY_ROOT=` + applyRoot + `
Environment=VEIL_LIVE_ROOT=` + path.Join(cfg.EtcDir, "generated") + `
ExecStart=` + cfg.VeilBinary + ` serve
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
ReadOnlyPaths=` + cfg.EtcDir + `
ReadWritePaths=` + cfg.VarDir + `

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
ExecStart=` + cfg.VeilBinary + ` helper serve --systemd-socket-activation
NoNewPrivileges=true
PrivateDevices=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=yes
CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
RestrictAddressFamilies=AF_UNIX AF_NETLINK
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
Environment=VEIL_STATE_PATH=` + statePath + `
Environment=VEIL_KEY_PATH=` + keyPath + `
Environment=VEIL_APPLY_ROOT=` + applyRoot + `
Environment=VEIL_LIVE_ROOT=` + path.Join(cfg.EtcDir, "generated") + `
ReadWritePaths=` + cfg.EtcDir + ` ` + cfg.VarDir + ` /usr/local/bin /etc/ufw /run /var/run
`,
		UnitHelperSocket: `[Unit]
Description=Veil privileged helper socket

[Socket]
ListenStream=/run/veil/helper.sock
Accept=no
SocketUser=root
SocketGroup=veil
SocketMode=0660
DirectoryMode=0750
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
SupplementaryGroups=veil veil-proxy
# Caddy stores its cert/key material and local CA root here. The hardening
# below drops CAP_DAC_OVERRIDE and /var/lib/veil is owned by the veil user, so
# Caddy (root) cannot write there; give it a dedicated state dir it owns
# (systemd creates /var/lib/caddy) for both ACME storage and the self-signed
# internal-CA fallback.
StateDirectory=caddy
Environment=HOME=/var/lib/caddy XDG_DATA_HOME=/var/lib/caddy XDG_CONFIG_HOME=/var/lib/caddy
ExecStart=` + cfg.CaddyBinary + ` run --config ` + caddyConfig + `
ExecReload=` + cfg.CaddyBinary + ` reload --config ` + caddyConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `ReadWritePaths=` + cfg.EtcDir + ` ` + cfg.VarDir + `

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
ExecStart=` + cfg.HysteriaBinary + ` server --config ` + hysteriaConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `InaccessiblePaths=/run/veil/helper.sock ` + cfg.VarDir + `

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
ExecStart=` + cfg.OlcrtcBinary + ` ` + olcrtcConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlockOlcrtc + `InaccessiblePaths=/run/veil/helper.sock ` + cfg.VarDir + `

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
ExecStart=` + cfg.SingBoxBinary + ` run -c ` + warpConfig + `
ExecReload=` + cfg.SingBoxBinary + ` check -c ` + warpConfig + `
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
InaccessiblePaths=/run/veil/helper.sock ` + cfg.VarDir + `

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
ExecStart=` + cfg.MieruBinary + ` run
ExecStartPost=/bin/sh -c 'i=0; while [ $$i -lt 50 ]; do if [ -S /run/veil-mieru/mita.sock ]; then ` + cfg.MieruBinary + ` apply config ` + mieruConfig + ` && ` + cfg.MieruBinary + ` start && exit 0; fi; i=$$((i+1)); sleep 0.2; done; echo "mita activation timed out" >&2; exit 1'
ExecStop=` + cfg.MieruBinary + ` stop
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `InaccessiblePaths=/run/veil/helper.sock ` + cfg.VarDir + `

[Install]
WantedBy=multi-user.target
`,
		UnitBackupService: `[Unit]
Description=Veil encrypted state backup
Documentation=https://github.com/mikkelchokolate/Veil/blob/main/docs/disaster-recovery.md
ConditionPathExists=` + path.Join(cfg.EtcDir, "backup.passphrase") + `
After=local-fs.target

[Service]
Type=oneshot
EnvironmentFile=-` + path.Join(cfg.EtcDir, "veil.env") + `
ExecStart=` + cfg.VeilBinary + ` backup create --state ` + statePath + ` --key-path ` + keyPath + ` --passphrase-file ` + path.Join(cfg.EtcDir, "backup.passphrase") + ` --output-dir ` + backupDir + ` --prune --daily 7 --weekly 4 --monthly 12
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
ReadWritePaths=` + cfg.VarDir + `
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
	switch name {
	case UnitVeil, UnitHelperService, UnitBackupService, UnitCaddy:
		return "[Service]\n" + dropInServiceOverrides(name, cfg), true
	default:
		return "", false
	}
}

func dropInServiceOverrides(name string, cfg SystemdConfig) string {
	if cfg.EtcDir == "" {
		cfg.EtcDir = "/etc/veil"
	}
	if cfg.VarDir == "" {
		cfg.VarDir = "/var/lib/veil"
	}
	if cfg.VeilBinary == "" {
		cfg.VeilBinary = "/usr/local/bin/veil"
	}
	var b strings.Builder
	switch name {
	case UnitVeil:
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + cfg.VeilBinary + " serve\n")
		b.WriteString("EnvironmentFile=-" + path.Join(cfg.EtcDir, "veil.env") + "\n")
		b.WriteString("Environment=VEIL_STATE_PATH=" + path.Join(cfg.VarDir, "state.json") + "\n")
		b.WriteString("Environment=VEIL_KEY_PATH=" + path.Join(cfg.EtcDir, "state.key") + "\n")
		b.WriteString("Environment=VEIL_APPLY_ROOT=" + path.Join(cfg.VarDir, "staging") + "\n")
		b.WriteString("Environment=VEIL_LIVE_ROOT=" + path.Join(cfg.EtcDir, "generated") + "\n")
		b.WriteString("ReadOnlyPaths=" + cfg.EtcDir + "\n")
		b.WriteString("ReadWritePaths=" + cfg.VarDir + "\n")
	case UnitHelperService:
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + cfg.VeilBinary + " helper serve --systemd-socket-activation\n")
		b.WriteString("Environment=VEIL_STATE_PATH=" + path.Join(cfg.VarDir, "state.json") + "\n")
		b.WriteString("Environment=VEIL_KEY_PATH=" + path.Join(cfg.EtcDir, "state.key") + "\n")
		b.WriteString("Environment=VEIL_APPLY_ROOT=" + path.Join(cfg.VarDir, "staging") + "\n")
		b.WriteString("Environment=VEIL_LIVE_ROOT=" + path.Join(cfg.EtcDir, "generated") + "\n")
		b.WriteString("ReadWritePaths=" + cfg.EtcDir + " " + cfg.VarDir + " /usr/local/bin /etc/ufw /run /var/run\n")
	case UnitBackupService:
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + cfg.VeilBinary + " backup create --state " + path.Join(cfg.VarDir, "state.json") + " --key-path " + path.Join(cfg.EtcDir, "state.key") + " --passphrase-file " + path.Join(cfg.EtcDir, "backup.passphrase") + " --output-dir " + path.Join(cfg.VarDir, "backups") + " --prune --daily 7 --weekly 4 --monthly 12\n")
		b.WriteString("EnvironmentFile=-" + path.Join(cfg.EtcDir, "veil.env") + "\n")
		b.WriteString("ReadWritePaths=" + cfg.VarDir + "\n")
	case UnitCaddy:
		caddyBin := cfg.CaddyBinary
		if caddyBin == "" {
			caddyBin = "/usr/local/bin/caddy"
		}
		config := path.Join(cfg.EtcDir, "generated", "caddy", "config.json")
		b.WriteString("ExecStart=\n")
		b.WriteString("ExecStart=" + caddyBin + " run --config " + config + "\n")
		b.WriteString("ExecReload=\n")
		b.WriteString("ExecReload=" + caddyBin + " reload --config " + config + "\n")
		b.WriteString("ReadWritePaths=" + cfg.EtcDir + " " + cfg.VarDir + "\n")
	}
	return b.String()
}
