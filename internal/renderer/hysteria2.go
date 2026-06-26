package renderer

import (
	"bytes"
	"errors"
	"text/template"
)

type Hysteria2User struct {
	Username string
	Password string
}

type Hysteria2Config struct {
	ListenPort    int
	Domain        string
	Password      string
	Users         []Hysteria2User
	MasqueradeURL string
	Upstream      string
	// CertPath/KeyPath point at a TLS cert/key Hysteria2 serves. Defaults to
	// Veil's managed self-signed panel cert, so Hysteria2 works on any host
	// (bare IP, no domain, ports 80/443 taken) without ACME — the client
	// connects with insecure/SNI. This is what makes "enter a domain and it
	// just works" hold for Hysteria2.
	CertPath string
	KeyPath  string
}

func RenderHysteria2(cfg Hysteria2Config) (string, error) {
	if cfg.ListenPort <= 0 {
		return "", errors.New("listen port is required")
	}
	if len(cfg.Users) == 0 {
		if cfg.Password == "" {
			return "", errors.New("password is required")
		}
	} else {
		for _, user := range cfg.Users {
			if user.Username == "" || user.Password == "" {
				return "", errors.New("username and password are required")
			}
		}
	}
	if cfg.MasqueradeURL == "" {
		cfg.MasqueradeURL = "https://www.bing.com/"
	}
	if cfg.CertPath == "" {
		cfg.CertPath = "/etc/veil/panel/tls.crt"
	}
	if cfg.KeyPath == "" {
		cfg.KeyPath = "/etc/veil/panel/tls.key"
	}
	const tpl = `listen: :{{ .ListenPort }}

# Self-signed TLS from Veil's managed cert — no ACME, works on any host.
# Clients connect with insecure + SNI (see the generated client link).
tls:
  cert: {{ .CertPath }}
  key: {{ .KeyPath }}

# Password authentication is simple and broadly compatible with Hysteria2 clients.
auth:
{{- if .Users }}
  type: userpass
  userpass:
{{- range .Users }}
    {{ .Username }}: {{ .Password }}
{{- end }}
{{- else }}
  type: password
  password: {{ .Password }}
{{- end }}

masquerade:
  type: proxy
  proxy:
    url: {{ .MasqueradeURL }}
    rewriteHost: true

{{- if .Upstream }}
outbound:
  type: socks5
  socks5:
    addr: {{ .Upstream }}
{{- end }}
`
	var out bytes.Buffer
	if err := template.Must(template.New("hysteria2").Parse(tpl)).Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}
