package renderer

import (
	"bytes"
	"errors"
	"text/template"
)

type OlcrtcConfig struct {
	Auth      string
	RoomID    string
	Key       string
	Transport string
	DNS       string
}

func RenderOlcrtc(cfg OlcrtcConfig) (string, error) {
	if cfg.Key == "" {
		return "", errors.New("crypto key is required")
	}
	if cfg.Transport == "" {
		cfg.Transport = "datachannel"
	}
	if cfg.DNS == "" {
		cfg.DNS = "1.1.1.1:53"
	}
	const tpl = `mode: srv
auth:
  provider: {{ .Auth }}
room:
  id: "{{ .RoomID }}"
crypto:
  key: "{{ .Key }}"
net:
  transport: {{ .Transport }}
  dns: "{{ .DNS }}"
data: data
`
	var out bytes.Buffer
	t, err := template.New("olcrtc").Parse(tpl)
	if err != nil {
		return "", err
	}
	if err := t.Execute(&out, cfg); err != nil {
		return "", err
	}
	return out.String(), nil
}
