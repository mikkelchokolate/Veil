package renderer

import (
	"bytes"
	"errors"
	"strings"
	"text/template"
)

type PanelCaddyConfig struct {
	Domain           string
	Email            string
	PanelPort        int
	PublicPort       int
	WebBasePath      string
	WebBasePathSlash string
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
	if cfg.PublicPort == 0 {
		cfg.PublicPort = 443
	}
	// Preserve trailing slash semantics used by the Panel router while also
	// providing a no-slash variant for the exact-path redirect.
	cfg.WebBasePathSlash = cfg.WebBasePath
	if !strings.HasSuffix(cfg.WebBasePathSlash, "/") {
		cfg.WebBasePathSlash += "/"
	}
	cfg.WebBasePath = strings.TrimSuffix(cfg.WebBasePath, "/")
	// TLS: try ACME first (a real, browser-trusted cert — best masking, no
	// warnings), and automatically fall back to Caddy's internal self-signed CA
	// if ACME can't issue (no public DNS, ports unreachable, rate limits). So
	// the operator just enters a domain and it always works, using a trusted
	// cert whenever possible.
	const tpl = `{{ .Domain }}{{ if ne .PublicPort 443 }}:{{ .PublicPort }}{{ end }} {
  tls {
    issuer acme{{ if .Email }} {
      email {{ .Email }}
    }{{ end }}
    issuer internal
  }

  handle {{ .WebBasePath }} {
    redir * {{ .WebBasePathSlash }} 308
  }

  handle {{ .WebBasePathSlash }}* {
    reverse_proxy 127.0.0.1:{{ .PanelPort }}
  }

  handle {
    header Cache-Control "no-store"
    header Pragma "no-cache"
    respond "Veil Panel" 404
  }
}
`
	var out bytes.Buffer
	if err := template.Must(template.New("panel-caddy").Parse(tpl)).Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}
