package renderer

import (
	"bytes"
	"errors"

	"gopkg.in/yaml.v3"
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
	CertPath           string
	KeyPath            string
	TrafficStatsListen string
	TrafficStatsSecret string
}

type hysteria2YAML struct {
	Listen string `yaml:"listen"`
	TLS    struct {
		Cert string `yaml:"cert"`
		Key  string `yaml:"key"`
	} `yaml:"tls"`
	Auth struct {
		Type     string            `yaml:"type"`
		Password string            `yaml:"password,omitempty"`
		UserPass map[string]string `yaml:"userpass,omitempty"`
	} `yaml:"auth"`
	Masquerade struct {
		Type  string `yaml:"type"`
		Proxy struct {
			URL         string `yaml:"url"`
			RewriteHost bool   `yaml:"rewriteHost"`
		} `yaml:"proxy"`
	} `yaml:"masquerade"`
	Outbounds []struct {
		Name   string `yaml:"name"`
		Type   string `yaml:"type"`
		Socks5 struct {
			Addr string `yaml:"addr"`
		} `yaml:"socks5"`
	} `yaml:"outbounds,omitempty"`
	TrafficStats *struct {
		Listen string `yaml:"listen"`
		Secret string `yaml:"secret"`
	} `yaml:"trafficStats,omitempty"`
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

	var doc hysteria2YAML
	doc.Listen = ":" + itoa(cfg.ListenPort)
	doc.TLS.Cert = cfg.CertPath
	doc.TLS.Key = cfg.KeyPath
	if len(cfg.Users) > 0 {
		doc.Auth.Type = "userpass"
		doc.Auth.UserPass = make(map[string]string, len(cfg.Users))
		for _, u := range cfg.Users {
			doc.Auth.UserPass[u.Username] = u.Password
		}
	} else {
		doc.Auth.Type = "password"
		doc.Auth.Password = cfg.Password
	}
	doc.Masquerade.Type = "proxy"
	doc.Masquerade.Proxy.URL = cfg.MasqueradeURL
	doc.Masquerade.Proxy.RewriteHost = true
	if cfg.Upstream != "" {
		var ob struct {
			Name   string `yaml:"name"`
			Type   string `yaml:"type"`
			Socks5 struct {
				Addr string `yaml:"addr"`
			} `yaml:"socks5"`
		}
		ob.Name = "veil-upstream"
		ob.Type = "socks5"
		ob.Socks5.Addr = cfg.Upstream
		doc.Outbounds = append(doc.Outbounds, ob)
	}

	if cfg.TrafficStatsListen != "" || cfg.TrafficStatsSecret != "" {
		if cfg.TrafficStatsListen == "" || cfg.TrafficStatsSecret == "" {
			return "", errors.New("traffic stats listen and secret must be configured together")
		}
		doc.TrafficStats = &struct {
			Listen string `yaml:"listen"`
			Secret string `yaml:"secret"`
		}{Listen: cfg.TrafficStatsListen, Secret: cfg.TrafficStatsSecret}
	}

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
