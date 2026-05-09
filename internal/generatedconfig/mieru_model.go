package generatedconfig

import "github.com/veil-panel/veil/internal/renderer"

type MieruGeneratedConfigModel struct {
	settings Settings
}

func NewMieruGeneratedConfigModel(settings Settings) MieruGeneratedConfigModel {
	return MieruGeneratedConfigModel{settings: settings}
}

func (m MieruGeneratedConfigModel) Build(inbounds []Inbound) (renderer.MieruConfig, bool, error) {
	config := renderer.MieruConfig{}
	for _, inbound := range inbounds {
		if !m.includes(inbound) {
			continue
		}
		config.PortBindings = append(config.PortBindings, renderer.MieruPortBinding{Port: inbound.Port, Protocol: inbound.Transport})
		credentials, err := BuildClientCredentials(inbound)
		if err != nil {
			return renderer.MieruConfig{}, false, err
		}
		if len(credentials) == 0 {
			config.Users = append(config.Users, renderer.MieruUser{Name: inbound.Name, Password: inbound.Password})
			continue
		}
		for _, credential := range credentials {
			config.Users = append(config.Users, renderer.MieruUser{Name: credential.Username, Password: credential.Password})
		}
	}
	if len(config.PortBindings) == 0 {
		return renderer.MieruConfig{}, false, nil
	}
	return config, true, nil
}

func (m MieruGeneratedConfigModel) includes(inbound Inbound) bool {
	return inbound.Enabled && inbound.Protocol == "mieru"
}
