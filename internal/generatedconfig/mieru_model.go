package generatedconfig

import (
	"errors"
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/model"
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
			// A credential-less inbound (e.g. restored from an old backup that
			// predates credential_required validation) must NOT render a user
			// with an empty password — anyone guessing the inbound name could
			// authenticate, and the client-access aggregator already skips the
			// same fallback when the password resolves empty. Fail the build
			// honestly instead (#1062); the message matches RenderMieru's
			// user check so every consumer reports the same contract.
			password := mieruEffectivePassword(inbound)
			if password == "" {
				return renderer.MieruConfig{}, false, errors.New("mieru user name and password are required")
			}
			if err := addUser(inbound.Name, password); err != nil {
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

// mieruEffectivePassword resolves the fallback credential the server renders.
// The same bytes must reach the exported client config, so resolution shares
// model.EffectiveInboundPassword and never trims the winning value (audit #311).
func mieruEffectivePassword(inbound Inbound) string {
	return model.EffectiveInboundPassword(inbound)
}

func hasProfiles(inbound Inbound) bool {
	return len(inbound.Profiles) > 0
}

func (m MieruGeneratedConfigModel) includes(inbound Inbound) bool {
	return inbound.Enabled && inbound.Protocol == "mieru"
}
