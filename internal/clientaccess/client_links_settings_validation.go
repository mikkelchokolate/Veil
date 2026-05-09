package clientaccess

import (
	"errors"
	"strings"
)

type ClientLinksSettingsValidation struct{}

func NewClientLinksSettingsValidation() ClientLinksSettingsValidation {
	return ClientLinksSettingsValidation{}
}

func (ClientLinksSettingsValidation) Validate(settings Settings) error {
	if strings.TrimSpace(settings.Domain) == "" {
		return errors.New("domain is required to build client links")
	}
	return nil
}
