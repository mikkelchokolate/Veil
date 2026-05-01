package renderer

import "path/filepath"

type SystemdConfig struct {
	VeilBinary     string
	CaddyBinary    string
	HysteriaBinary string
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
	if cfg.EtcDir == "" {
		cfg.EtcDir = "/etc/veil"
	}
	caddyfile := filepath.Join(cfg.EtcDir, "generated", "caddy", "Caddyfile")
	hysteriaConfig := filepath.Join(cfg.EtcDir, "generated", "hysteria2", "server.yaml")
	return map[string]string{
		"veil.service": `[Unit]
Description=Veil panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + cfg.VeilBinary + ` serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`,
		"veil-naive.service": `[Unit]
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
		"veil-hysteria2.service": `[Unit]
Description=Veil managed Hysteria2
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
	}
}
