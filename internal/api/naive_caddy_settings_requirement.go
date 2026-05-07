package api

import "strings"

type NaiveCaddySettingsRequirement struct{}

func NewNaiveCaddySettingsRequirement() NaiveCaddySettingsRequirement {
	return NaiveCaddySettingsRequirement{}
}

func (NaiveCaddySettingsRequirement) Validate(settings Settings) error {
	if strings.TrimSpace(settings.Domain) == "" || strings.TrimSpace(settings.Email) == "" || strings.TrimSpace(settings.NaiveUsername) == "" || strings.TrimSpace(settings.NaivePassword) == "" {
		return errNaiveCaddySettingsRequired{}
	}
	return nil
}

type errNaiveCaddySettingsRequired struct{}

func (errNaiveCaddySettingsRequired) Error() string {
	return "domain, email, naive username, and naive password are required for NaiveProxy/Caddy"
}
