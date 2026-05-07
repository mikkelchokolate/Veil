package api

import (
	"encoding/json"
	"strings"
)

type MieruClientConfig struct{}

func NewMieruClientConfig() MieruClientConfig { return MieruClientConfig{} }

func (MieruClientConfig) Build(settings Settings, inbound Inbound, linkName string, credential ClientCredential) (string, error) {
	body := struct {
		ProfileName string              `json:"profileName"`
		User        mieruClientUser     `json:"user"`
		Servers     []mieruClientServer `json:"servers"`
	}{
		ProfileName: linkName,
		User:        mieruClientUser{Name: credential.Username, Password: credential.Password},
		Servers: []mieruClientServer{{
			DomainName: settings.Domain,
			PortBindings: []mieruClientPortBinding{{
				Port:     inbound.Port,
				Protocol: strings.ToUpper(inbound.Transport),
			}},
		}},
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

type mieruClientUser struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type mieruClientServer struct {
	DomainName   string                   `json:"domainName"`
	PortBindings []mieruClientPortBinding `json:"portBindings"`
}

type mieruClientPortBinding struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}
