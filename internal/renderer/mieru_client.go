package renderer

import "encoding/json"

type MieruClientConfig struct {
	ProfileName  string
	DomainName   string
	PortBindings []MieruPortBinding
	User         MieruUser
}

type mieruClientConfigJSON struct {
	ProfileName string              `json:"profileName"`
	User        mieruUserJSON       `json:"user"`
	Servers     []mieruClientServer `json:"servers"`
}

type mieruClientServer struct {
	DomainName   string                 `json:"domainName"`
	PortBindings []mieruPortBindingJSON `json:"portBindings"`
}

func RenderMieruClient(cfg MieruClientConfig) (string, error) {
	body := mieruClientConfigJSON{
		ProfileName: cfg.ProfileName,
		User:        mieruUserJSON{Name: cfg.User.Name, Password: cfg.User.Password},
		Servers: []mieruClientServer{{
			DomainName:   cfg.DomainName,
			PortBindings: mieruClientPortBindings(cfg.PortBindings),
		}},
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

func mieruClientPortBindings(bindings []MieruPortBinding) []mieruPortBindingJSON {
	out := make([]mieruPortBindingJSON, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, mieruPortBindingJSON{Port: binding.Port, Protocol: normalizeMieruProtocol(binding.Protocol)})
	}
	return out
}
