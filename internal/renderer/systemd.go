package renderer

import "path"

const (
	UnitVeil      = "veil.service"
	UnitNaive     = "veil-naive.service"
	UnitHysteria2 = "veil-hysteria2@.service"
	UnitOlcrtc    = "veil-olcrtc@.service"
	UnitWarp      = "veil-warp.service"
	UnitMieru     = "veil-mieru.service"
)

func ManagedSystemdUnitNames() []string {
	return []string{UnitVeil, UnitNaive, UnitHysteria2, UnitOlcrtc, UnitWarp, UnitMieru}
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
	caddyfile := path.Join(cfg.EtcDir, "generated", "caddy", "Caddyfile")
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

[Install]
WantedBy=multi-user.target
`,
		UnitNaive: `[Unit]
Description=Veil managed NaiveProxy/Caddy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + cfg.CaddyBinary + ` run --config ` + caddyfile + ` --adapter caddyfile
ExecReload=` + cfg.CaddyBinary + ` reload --config ` + caddyfile + ` --adapter caddyfile
Restart=on-failure
RestartSec=3

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

[Install]
WantedBy=multi-user.target
`,
	}
}
