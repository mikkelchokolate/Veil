package clientaccess

type ClientLinksSettingsValidation struct{}

func NewClientLinksSettingsValidation() ClientLinksSettingsValidation {
	return ClientLinksSettingsValidation{}
}

func (ClientLinksSettingsValidation) Validate(settings Settings) error {
	return nil
}
