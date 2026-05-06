package api

import "errors"

type ClientCredential struct {
	Name     string
	Username string
	Password string
}

func BuildClientCredentials(inbound Inbound) ([]ClientCredential, error) {
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
