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
	seenBindings := make(map[mieruPortBindingJSON]struct{}, len(cfg.PortBindings))
	for _, binding := range cfg.PortBindings {
		if binding.Port < 1 || binding.Port > 65535 {
			return "", errors.New("mieru port must be between 1 and 65535")
		}
		protocol := normalizeMieruProtocol(binding.Protocol)
		if protocol != "TCP" && protocol != "UDP" {
			return "", errors.New("mieru protocol must be TCP or UDP")
		}
		candidate := mieruPortBindingJSON{Port: binding.Port, Protocol: protocol}
		if _, exists := seenBindings[candidate]; exists {
			continue
		}
		seenBindings[candidate] = struct{}{}
		out.PortBindings = append(out.PortBindings, candidate)
	}
	seenUsers := make(map[string]string, len(cfg.Users))
	for _, user := range cfg.Users {
		if user.Name == "" || user.Password == "" {
			return "", errors.New("mieru user name and password are required")
		}
		if password, exists := seenUsers[user.Name]; exists {
			if password != user.Password {
				return "", errors.New("mieru user name has conflicting passwords")
			}
			continue
		}
		seenUsers[user.Name] = user.Password
		out.Users = append(out.Users, mieruUserJSON(user))
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}
