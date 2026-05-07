package api

type ClientAccessFallbackCredential struct{}

func NewClientAccessFallbackCredential() ClientAccessFallbackCredential {
	return ClientAccessFallbackCredential{}
}

func (ClientAccessFallbackCredential) Build(settings Settings, inbound Inbound) ClientCredential {
	credential := ClientCredential{Name: inbound.Name, Username: inbound.Name, Password: inbound.Password}
	switch inbound.Protocol {
	case "naiveproxy":
		credential.Username = settings.NaiveUsername
		if credential.Password == "" {
			credential.Password = settings.NaivePassword
		}
	case "hysteria2":
		if credential.Password == "" {
			credential.Password = settings.Hysteria2Password
		}
	}
	return credential
}
