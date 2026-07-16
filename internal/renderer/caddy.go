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
	if cfg.ListenPort > 65535 {
		return "", errors.New("listen port must be between 1 and 65535")
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
	cfg.FallbackRoot = filepath.Clean(cfg.FallbackRoot)
	if !strings.HasPrefix(filepath.ToSlash(cfg.FallbackRoot), "/var/lib/veil") {
		cfg.FallbackRoot = filepath.Clean("/var/lib/veil/" + cfg.FallbackRoot)
	}
	if !strings.HasPrefix(filepath.ToSlash(cfg.FallbackRoot), "/var/lib/veil") {
		return "", fmt.Errorf("fallback root must be within /var/lib/veil: %s", cfg.FallbackRoot)
	}
	cfg.FallbackRoot = filepath.ToSlash(cfg.FallbackRoot)

	// Keep this layout aligned with the NaiveProxy upstream server example.
	// In particular, :port must be the first site address and production must
	// never silently fall back to an internally-issued certificate: Chromium-
	// based Naive clients require a publicly trusted TLS chain.
	const tpl = `{
  order forward_proxy before file_server
  log {
    exclude http.log.error
  }
  servers {
    protocols h1 h2
  }
}

:{{ .ListenPort }}, {{ .Domain }} {
  tls {
    issuer acme{{ if .Email }} {
      email {{ .Email }}
    }{{ end }}
  }
  encode

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

{{- if .PanelPort }}
{{- if .WebBasePath }}
  handle {{ .WebBasePath }}* {
    reverse_proxy 127.0.0.1:{{ .PanelPort }}
  }
{{- end }}
{{- end }}

  root * {{ .FallbackRoot }}
  file_server
}
`
	var out bytes.Buffer
	if err := template.Must(template.New("caddy").Parse(tpl)).Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}
