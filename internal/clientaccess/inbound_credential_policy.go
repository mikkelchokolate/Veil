package clientaccess

import "errors"

type InboundCredentialPolicy struct {
	generate InboundPasswordGenerator
}

func NewInboundCredentialPolicy(generate InboundPasswordGenerator) InboundCredentialPolicy {
	if generate == nil {
		generate = generateInboundPassword
	}
	return InboundCredentialPolicy{generate: generate}
}

func (p InboundCredentialPolicy) ApplyCreate(inbound *Inbound) {
	if inbound == nil {
		return
	}
	if inbound.Password == "" && len(inbound.Profiles) == 0 {
		inbound.Password = p.generate()
	}
	p.completeProfilePasswords(inbound, nil)
}

func (p InboundCredentialPolicy) ApplyUpdate(inbound *Inbound, previous Inbound) {
	if inbound == nil {
		return
	}
	if inbound.Password == "" {
		inbound.Password = previous.Password
	}
	p.completeProfilePasswords(inbound, previous.Profiles)
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

func (p InboundCredentialPolicy) completeProfilePasswords(inbound *Inbound, previous []ClientProfile) {
	inbound.Profiles = NewClientProfileCatalogWithPasswordGenerator(inbound.Profiles, p.generate).WithCompletedPasswords(previous)
}
