package renderer

import (
	"bytes"
	"errors"
	"text/template"
)

type Hysteria2Config struct {
	ListenPort    int
	Domain        string
	Password      string
	MasqueradeURL string
}

func RenderHysteria2(cfg Hysteria2Config) (string, error) {
	if cfg.ListenPort <= 0 {
		return "", errors.New("listen port is required")
	}
	if cfg.Password == "" {
		return "", errors.New("password is required")
	}
	if cfg.MasqueradeURL == "" {
		cfg.MasqueradeURL = "https://www.bing.com/"
	}
	const tpl = `listen: :{{ .ListenPort }}

acme:
  domains:
    - {{ .Domain }}

# Password authentication is simple and broadly compatible with Hysteria2 clients.
auth:
  type: password
  password: {{ .Password }}

masquerade:
  type: proxy
  proxy:
    url: {{ .MasqueradeURL }}
    rewriteHost: true
`
	var out bytes.Buffer
	if err := template.Must(template.New("hysteria2").Parse(tpl)).Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}
