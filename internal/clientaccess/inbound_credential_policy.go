package clientaccess

import (
	"errors"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type InboundCredentialPolicy struct {
	generate InboundPasswordGenerator
}

func NewInboundCredentialPolicy(generate InboundPasswordGenerator) InboundCredentialPolicy {
	if generate == nil {
		generate = generateInboundPassword
	}
	return InboundCredentialPolicy{generate: generate}
}

func (p InboundCredentialPolicy) ApplyCreate(inbound *Inbound) error {
	if inbound == nil {
		return nil
	}
	if model.EffectiveInboundPassword(*inbound) == "" && len(inbound.Profiles) == 0 {
		password, err := p.generate()
		if err != nil {
			return err
		}
		inbound.Password = password
	}
	return p.completeProfilePasswords(inbound, nil)
}

func (p InboundCredentialPolicy) ApplyUpdate(inbound *Inbound, previous Inbound) error {
	if inbound == nil {
		return nil
	}
	if model.EffectiveInboundPassword(*inbound) == "" {
		inbound.Password = previous.Password
	}
	return p.completeProfilePasswords(inbound, previous.Profiles)
}

func (p InboundCredentialPolicy) ClientCredentials(inbound Inbound) ([]ClientCredential, error) {
	profiles := NewClientProfileCatalog(inbound.Profiles).Enabled()
	credentials := make([]ClientCredential, 0, len(profiles))
	for _, profile := range profiles {
		username := profile.Username
		if username == "" {
			username = profile.Name
		}
		if username == "" || profile.Password == "" {
			return nil, errors.New("client profile username and password are required")
		}
		credentials = append(credentials, ClientCredential{Name: profile.Name, Username: username, Password: profile.Password})
	}
	return credentials, nil
}

func (p InboundCredentialPolicy) completeProfilePasswords(inbound *Inbound, previous []ClientProfile) error {
	completed, err := NewClientProfileCatalogWithPasswordGenerator(inbound.Profiles, p.generate).WithCompletedPasswords(previous)
	if err != nil {
		return err
	}
	inbound.Profiles = completed
	return nil
}
