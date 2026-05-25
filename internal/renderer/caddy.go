package renderer

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	if cfg.FallbackRoot == "" {
		cfg.FallbackRoot = "/var/lib/veil/www"
	}
	// Validate FallbackRoot is within /var/lib/veil to prevent path traversal.
	cfg.FallbackRoot = filepath.Clean(cfg.FallbackRoot)
	// Use ToSlash for platform-independent path manipulation.
	if !strings.HasPrefix(filepath.ToSlash(cfg.FallbackRoot), "/var/lib/veil") {
		cfg.FallbackRoot = filepath.Clean("/var/lib/veil/" + cfg.FallbackRoot)
	}
	if !strings.HasPrefix(filepath.ToSlash(cfg.FallbackRoot), "/var/lib/veil") {
		return "", fmt.Errorf("fallback root must be within /var/lib/veil: %s", cfg.FallbackRoot)
	}
	cfg.FallbackRoot = filepath.ToSlash(cfg.FallbackRoot)
	const tpl = `{
  order forward_proxy before file_server
  servers {
    protocols h1 h2
  }
}

:{{ .ListenPort }}, {{ .Domain }} {
  tls {{ .Email }}

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
  handle_path {{ .WebBasePath }}* {
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
