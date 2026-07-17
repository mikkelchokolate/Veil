package renderer

import (
	"bytes"
	"errors"

	"gopkg.in/yaml.v3"
)

type OlcrtcConfig struct {
	Auth      string
	RoomID    string
	Key       string
	Transport string
	DNS       string
}

type olcrtcYAML struct {
	Mode string `yaml:"mode"`
	Auth struct {
		Provider string `yaml:"provider"`
	} `yaml:"auth"`
	Room struct {
		ID string `yaml:"id"`
	} `yaml:"room"`
	Crypto struct {
		Key string `yaml:"key"`
	} `yaml:"crypto"`
	Net struct {
		Transport string `yaml:"transport"`
		DNS       string `yaml:"dns"`
	} `yaml:"net"`
	Data string `yaml:"data"`
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
	doc.Crypto.Key = cfg.Key
	doc.Net.Transport = cfg.Transport
	doc.Net.DNS = cfg.DNS
	doc.Data = "data"

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
