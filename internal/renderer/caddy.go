package renderer

import (
	"bytes"
	"errors"
	"text/template"
)

type NaiveUser struct {
	Username string
	Password string
}

type NaiveConfig struct {
	Domain       string
	Email        string
	ListenPort   int
	Username     string
	Password     string
	Users        []NaiveUser
	FallbackRoot string
	PanelPort    int
	WebBasePath  string
	Upstream     string
}

func RenderNaiveCaddyfile(cfg NaiveConfig) (string, error) {
	if cfg.Domain == "" {
		return "", errors.New("domain is required")
	}
	if cfg.ListenPort <= 0 {
		return "", errors.New("listen port is required")
	}
	if len(cfg.Users) == 0 {
		if cfg.Username == "" || cfg.Password == "" {
			return "", errors.New("naive username and password are required")
		}
		cfg.Users = []NaiveUser{{Username: cfg.Username, Password: cfg.Password}}
	}
	for _, user := range cfg.Users {
		if user.Username == "" || user.Password == "" {
			return "", errors.New("naive username and password are required")
		}
	}
	fallbackRoot, err := resolveNaiveFallbackRoot(cfg.FallbackRoot)
	if err != nil {
		return "", err
	}
	cfg.FallbackRoot = fallbackRoot
	const tpl = `{
  order forward_proxy before file_server
  servers {
    protocols h1 h2
  }
}

:{{ .ListenPort }}, {{ .Domain }} {
  tls {
    issuer acme{{ if .Email }} {
      email {{ .Email }}
    }{{ end }}
    issuer internal
  }

  forward_proxy {
{{- range .Users }}
    basic_auth {{ .Username }} {{ .Password }}
{{- end }}
    hide_ip
    hide_via
    probe_resistance
{{- if .Upstream }}
    upstream {{ .Upstream }}
{{- end }}
  }

  root * {{ .FallbackRoot }}
  file_server
{{- if .PanelPort }}
{{ if .WebBasePath }}
  handle {{ .WebBasePath }}* {
    reverse_proxy 127.0.0.1:{{ .PanelPort }}
  }
{{- end }}
{{- end }}
}
`
	var out bytes.Buffer
	if err := template.Must(template.New("caddy").Parse(tpl)).Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}
