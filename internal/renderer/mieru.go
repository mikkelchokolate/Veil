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
	MTU          int                    `json:"mtu,omitempty"`
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
		// mita rejects privileged and out-of-range ports. Fail during the Veil
		// apply plan instead of discovering the error only after service restart.
		if binding.Port < 1025 || binding.Port > 65535 {
			return "", errors.New("mieru port must be between 1025 and 65535")
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
	seenUsers := make(map[string]struct{}, len(cfg.Users))
	for _, user := range cfg.Users {
		if user.Name == "" || user.Password == "" {
			return "", errors.New("mieru user name and password are required")
		}
		if _, exists := seenUsers[user.Name]; exists {
			return "", errors.New("mieru user names must be unique")
		}
		seenUsers[user.Name] = struct{}{}
		out.Users = append(out.Users, mieruUserJSON(user))
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}
