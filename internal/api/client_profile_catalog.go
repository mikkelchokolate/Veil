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
	profiles := c.List()
	previousByName := map[string]ClientProfile{}
	for _, profile := range previous {
		previousByName[profile.Name] = profile
	}
	for i := range profiles {
		if profiles[i].Password != "" {
			continue
		}
		if previous, ok := previousByName[profiles[i].Name]; ok && previous.Password != "" {
			profiles[i].Password = previous.Password
			continue
		}
		profiles[i].Password = c.passwordGenerate()
	}
	return profiles
}

func cloneClientProfiles(profiles []ClientProfile) []ClientProfile {
	out := make([]ClientProfile, len(profiles))
	copy(out, profiles)
	return out
}
