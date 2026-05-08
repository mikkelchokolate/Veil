package renderer

import (
	"bytes"
	"errors"
	"text/template"
)

type PanelCaddyConfig struct {
	Domain      string
	Email       string
	PanelPort   int
	WebBasePath string
}

func RenderPanelCaddyfile(cfg PanelCaddyConfig) (string, error) {
	if cfg.Domain == "" {
		return "", errors.New("domain is required")
	}
	if cfg.PanelPort <= 0 {
		return "", errors.New("panel port is required")
	}
	if cfg.WebBasePath == "" || cfg.WebBasePath == "/" {
		return "", errors.New("web base path is required")
	}
	const tpl = `{{ .Domain }} {
  tls {{ .Email }}

  handle_path {{ .WebBasePath }}* {
    reverse_proxy 127.0.0.1:{{ .PanelPort }}
  }

  respond "Veil Panel" 404
}
`
	var out bytes.Buffer
	if err := template.Must(template.New("panel-caddy").Parse(tpl)).Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}
