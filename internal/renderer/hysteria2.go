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
	const tpl = `listen: :{{ .ListenPort }}

acme:
  domains:
    - {{ .Domain }}

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
