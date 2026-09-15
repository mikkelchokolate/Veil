package renderer

import (
	"bytes"
	"errors"

	"gopkg.in/yaml.v3"
)

type OlcrtcConfig struct {
	Auth      string
	RoomID    string
	Data      string
	Key       string
	Transport string
	DNS       string
	SocksAddr string
	SocksPort int
}

type olcrtcYAML struct {
	Mode string `yaml:"mode"`
	Auth struct {
		Provider string `yaml:"provider"`
	} `yaml:"auth"`
	Room struct {
		ID string `yaml:"id"`
	} `yaml:"room"`
	Data   string `yaml:"data,omitempty"`
	Crypto struct {
		Key string `yaml:"key"`
	} `yaml:"crypto"`
	Net struct {
		Transport string `yaml:"transport"`
		DNS       string `yaml:"dns"`
	} `yaml:"net"`
	Socks *olcrtcSocksYAML `yaml:"socks,omitempty"`
}

type olcrtcSocksYAML struct {
	ProxyAddr string `yaml:"proxy_addr"`
	ProxyPort int    `yaml:"proxy_port"`
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

	var doc olcrtcYAML
	doc.Mode = "srv"
	doc.Auth.Provider = cfg.Auth
	doc.Room.ID = cfg.RoomID
	doc.Data = cfg.Data
	doc.Crypto.Key = cfg.Key
	doc.Net.Transport = cfg.Transport
	doc.Net.DNS = cfg.DNS
	if cfg.SocksAddr != "" && cfg.SocksPort > 0 {
		doc.Socks = &olcrtcSocksYAML{ProxyAddr: cfg.SocksAddr, ProxyPort: cfg.SocksPort}
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
