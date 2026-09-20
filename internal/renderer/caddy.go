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
		cfg.FallbackRoot = NaiveDefaultFallbackRoot
	}
	cfg.FallbackRoot = filepath.Clean(cfg.FallbackRoot)
	if !strings.HasPrefix(filepath.ToSlash(cfg.FallbackRoot), "/") {
		for _, seg := range strings.Split(filepath.ToSlash(cfg.FallbackRoot), "/") {
			if seg == ".." {
				return "", fmt.Errorf("fallback root must not contain '..' path traversal: %s", cfg.FallbackRoot)
			}
		}
		cfg.FallbackRoot = filepath.Clean(NaiveDefaultFallbackRoot + "/" + cfg.FallbackRoot)
	}
	if !NaiveFallbackRootAllowed(filepath.ToSlash(cfg.FallbackRoot)) {
		return "", fmt.Errorf("fallback root must be within /etc/veil/www or /var/lib/veil: %s", cfg.FallbackRoot)
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

// NaiveDefaultFallbackRoot is the web root Caddy serves for naive fallback.
// It lives under /etc/veil because the caddy unit runs as veil-proxy with
// /var/lib/veil in InaccessiblePaths: a var-lib root would be unreachable
// (audit #497). It is root:veil-proxy 0750/0640 like generated/.
const NaiveDefaultFallbackRoot = "/etc/veil/www"

// NaiveFallbackRootAllowed enforces the fallback-root boundary: exactly
// /etc/veil/www or a subdirectory (never /etc/veil itself or siblings like
// panel/, which hold keys readable by veil-proxy), or the legacy
// /var/lib/veil subtree for configurations that still reference it
// (audit #77 F1/F4 boundary, moved for the veil-proxy runtime).
func NaiveFallbackRootAllowed(root string) bool {
	if root == "/var/lib/veil" {
		return false
	}
	if strings.HasPrefix(root, "/var/lib/veil/") {
		return true
	}
	return root == NaiveDefaultFallbackRoot || strings.HasPrefix(root, NaiveDefaultFallbackRoot+"/")
}
