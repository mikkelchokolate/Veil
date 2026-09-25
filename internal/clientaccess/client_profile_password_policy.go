package clientaccess

type ClientProfilePasswordPolicy struct {
	generate InboundPasswordGenerator
}

func NewClientProfilePasswordPolicy(generate InboundPasswordGenerator) ClientProfilePasswordPolicy {
	if generate == nil {
		generate = generateInboundPassword
	}
	return ClientProfilePasswordPolicy{generate: generate}
}

func (p ClientProfilePasswordPolicy) Complete(profiles []ClientProfile, previous []ClientProfile) ([]ClientProfile, error) {
	completed := cloneClientProfiles(profiles)
	previousByName := map[string]ClientProfile{}
	for _, profile := range previous {
		previousByName[profile.Name] = profile
	}
	for i := range completed {
		if completed[i].Password != "" {
			continue
		}
		if previous, ok := previousByName[completed[i].Name]; ok && previous.Password != "" {
			completed[i].Password = previous.Password
			continue
		}
		password, err := p.generate()
		if err != nil {
			return nil, err
		}
		completed[i].Password = password
	}
	return completed, nil
}
