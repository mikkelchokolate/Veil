package api

type ClientProfileCatalog struct {
	profiles         []ClientProfile
	passwordGenerate InboundPasswordGenerator
}

func NewClientProfileCatalog(profiles []ClientProfile) ClientProfileCatalog {
	return NewClientProfileCatalogWithPasswordGenerator(profiles, generateInboundPassword)
}

func NewClientProfileCatalogWithPasswordGenerator(profiles []ClientProfile, generator InboundPasswordGenerator) ClientProfileCatalog {
	if generator == nil {
		generator = generateInboundPassword
	}
	return ClientProfileCatalog{profiles: cloneClientProfiles(profiles), passwordGenerate: generator}
}

func (c ClientProfileCatalog) List() []ClientProfile {
	return cloneClientProfiles(c.profiles)
}

func (c ClientProfileCatalog) Enabled() []ClientProfile {
	profiles := []ClientProfile{}
	for _, profile := range c.profiles {
		if profile.Enabled {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

func (c ClientProfileCatalog) WithCompletedPasswords(previous []ClientProfile) []ClientProfile {
	return NewClientProfilePasswordPolicy(c.passwordGenerate).Complete(c.profiles, previous)
}

func cloneClientProfiles(profiles []ClientProfile) []ClientProfile {
	out := make([]ClientProfile, len(profiles))
	copy(out, profiles)
	return out
}
