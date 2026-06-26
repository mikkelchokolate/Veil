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
	// TLS: try ACME first (a real, browser-trusted cert — best masking, no
	// warnings), and automatically fall back to Caddy's internal self-signed CA
	// if ACME can't issue (no public DNS, ports unreachable, rate limits). So
	// the operator just enters a domain and it always works, using a trusted
	// cert whenever possible.
	const tpl = `{{ .Domain }} {
  tls {
    issuer acme{{ if .Email }} {
      email {{ .Email }}
    }{{ end }}
    issuer internal
  }

  handle {{ .WebBasePath }}* {
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
