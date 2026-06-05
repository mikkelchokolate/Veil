package renderer

import "path"

const (
	UnitVeil          = "veil.service"
	UnitCaddy         = "veil-caddy@.service"
	UnitHysteria2     = "veil-hysteria2@.service"
	UnitOlcrtc        = "veil-olcrtc@.service"
	UnitWarp          = "veil-warp.service"
	UnitMieru         = "veil-mieru.service"
	UnitBackupService = "veil-backup.service"
	UnitBackupTimer   = "veil-backup.timer"
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

func ManagedSystemdUnitNames() []string {
	return []string{UnitVeil, UnitCaddy, UnitHysteria2, UnitOlcrtc, UnitWarp, UnitMieru, UnitBackupService, UnitBackupTimer}
}

type SystemdConfig struct {
	VeilBinary     string
	CaddyBinary    string
	HysteriaBinary string
	SingBoxBinary  string
	MieruBinary    string
	OlcrtcBinary   string
	EtcDir         string
}

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
		cfg.MieruBinary = "/usr/local/bin/mieru"
	}
	if cfg.OlcrtcBinary == "" {
		cfg.OlcrtcBinary = "/usr/local/bin/olcrtc"
	}
	if cfg.EtcDir == "" {
		cfg.EtcDir = "/etc/veil"
	}
	caddyfile := path.Join(cfg.EtcDir, "generated", "caddy", "%i.Caddyfile")
	hysteriaConfig := path.Join(cfg.EtcDir, "generated", "hysteria2", "%i.yaml")
	olcrtcConfig := path.Join(cfg.EtcDir, "generated", "olcrtc", "%i.yaml")
	warpConfig := path.Join(cfg.EtcDir, "generated", "sing-box", "warp.json")
	mieruConfig := path.Join(cfg.EtcDir, "generated", "mieru", "server_config.json")
	return map[string]string{
		UnitVeil: `[Unit]
Description=Veil panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-` + path.Join(cfg.EtcDir, "veil.env") + `
ExecStart=` + cfg.VeilBinary + ` serve
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `ReadWritePaths=/etc/veil /var/lib/veil

[Install]
WantedBy=multi-user.target
`,
		UnitCaddy: `[Unit]
Description=Veil managed NaiveProxy/Caddy (%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + cfg.CaddyBinary + ` run --config ` + caddyfile + ` --adapter caddyfile
ExecReload=` + cfg.CaddyBinary + ` reload --config ` + caddyfile + ` --adapter caddyfile
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `ReadWritePaths=/etc/veil /var/lib/veil

[Install]
WantedBy=multi-user.target
`,
		UnitHysteria2: `[Unit]
Description=Veil managed Hysteria2 (%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + cfg.HysteriaBinary + ` server --config ` + hysteriaConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `ReadWritePaths=/etc/veil /var/lib/veil

[Install]
WantedBy=multi-user.target
`,
		UnitOlcrtc: `[Unit]
Description=Veil managed olcRTC (%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + cfg.OlcrtcBinary + ` --config ` + olcrtcConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `ReadWritePaths=/etc/veil /var/lib/veil

[Install]
WantedBy=multi-user.target
`,
		UnitWarp: `[Unit]
Description=Veil managed WARP/sing-box outbound
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + cfg.SingBoxBinary + ` run -c ` + warpConfig + `
ExecReload=` + cfg.SingBoxBinary + ` check -c ` + warpConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `ReadWritePaths=/etc/veil /var/lib/veil

[Install]
WantedBy=multi-user.target
`,
		UnitMieru: `[Unit]
Description=Veil managed Mieru
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + cfg.MieruBinary + ` run -c ` + mieruConfig + `
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
` + systemdHardeningBlock + `ReadWritePaths=/etc/veil /var/lib/veil

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
ExecStart=` + cfg.VeilBinary + ` backup create --state /var/lib/veil/state.json --key-path ` + path.Join(cfg.EtcDir, "state.key") + ` --passphrase-file ` + path.Join(cfg.EtcDir, "backup.passphrase") + ` --output-dir /var/lib/veil/backups --prune --daily 7 --weekly 4 --monthly 12
User=root
Group=root
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
PrivateDevices=true
CapabilityBoundingSet=
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
ReadWritePaths=/var/lib/veil/backups
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
