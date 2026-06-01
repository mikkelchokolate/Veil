package renderer

import (
	"encoding/json"
	"errors"
)

type MieruPortBinding struct {
	Port     int
	Protocol string
}

type MieruUser struct {
	Name     string
	Password string
}

type MieruConfig struct {
	PortBindings []MieruPortBinding
	Users        []MieruUser
}

type mieruServerConfigJSON struct {
	PortBindings []mieruPortBindingJSON `json:"portBindings"`
	Users        []mieruUserJSON        `json:"users"`
	LoggingLevel string                 `json:"loggingLevel"`
}

type mieruPortBindingJSON struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type mieruUserJSON struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func RenderMieru(cfg MieruConfig) (string, error) {
	if len(cfg.PortBindings) == 0 {
		return "", errors.New("at least one mieru port binding is required")
	}
	if len(cfg.Users) == 0 {
		return "", errors.New("at least one mieru user is required")
	}
	out := mieruServerConfigJSON{LoggingLevel: "INFO"}
	for _, binding := range cfg.PortBindings {
		if binding.Port <= 0 {
			return "", errors.New("mieru port is required")
		}
		protocol := normalizeMieruProtocol(binding.Protocol)
		if protocol != "TCP" && protocol != "UDP" {
			return "", errors.New("mieru protocol must be TCP or UDP")
		}
		out.PortBindings = append(out.PortBindings, mieruPortBindingJSON{Port: binding.Port, Protocol: protocol})
	}
	for _, user := range cfg.Users {
		if user.Name == "" || user.Password == "" {
			return "", errors.New("mieru user name and password are required")
		}
		out.Users = append(out.Users, mieruUserJSON(user))
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}
