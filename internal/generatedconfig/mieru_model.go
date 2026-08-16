package generatedconfig

import (
	"fmt"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type MieruGeneratedConfigModel struct {
	settings Settings
}

func NewMieruGeneratedConfigModel(settings Settings) MieruGeneratedConfigModel {
	return MieruGeneratedConfigModel{settings: settings}
}

func (m MieruGeneratedConfigModel) Build(inbounds []Inbound) (renderer.MieruConfig, bool, error) {
	config := renderer.MieruConfig{}
	seen := map[string]struct{}{}
	addUser := func(name, password string) error {
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate mieru user name %q across enabled mieru inbounds", name)
		}
		seen[name] = struct{}{}
		config.Users = append(config.Users, renderer.MieruUser{Name: name, Password: password})
		return nil
	}
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
			// Fall back to the inbound credential only when the inbound has no
			// client profiles at all. If profiles exist but every one of them
			// is disabled, the user deliberately revoked all clients: falling
			// back would silently re-enable the legacy inbound user.
			if hasProfiles(inbound) {
				continue
			}
			if err := addUser(inbound.Name, mieruEffectivePassword(inbound)); err != nil {
				return renderer.MieruConfig{}, false, err
			}
			continue
		}
		for _, credential := range credentials {
			if err := addUser(credential.Username, credential.Password); err != nil {
				return renderer.MieruConfig{}, false, err
			}
		}
	}
	if len(config.PortBindings) == 0 {
		return renderer.MieruConfig{}, false, nil
	}
	return config, true, nil
}

func mieruEffectivePassword(inbound Inbound) string {
	if inbound.ProtocolFields != nil {
		if value, ok := inbound.ProtocolFields["password"].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return strings.TrimSpace(inbound.Password)
}

func hasProfiles(inbound Inbound) bool {
	return len(inbound.Profiles) > 0
}

func (m MieruGeneratedConfigModel) includes(inbound Inbound) bool {
	return inbound.Enabled && inbound.Protocol == "mieru"
}
